package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/nats-io/nats.go"
	"github.com/rivo/tview"
	"github.com/samber/lo"
	"gonum.org/v1/gonum/stat"
)

func main() {
	app := tview.NewApplication()
	contentPages := tview.NewPages()

	// funcSelect
	funcSelect := tview.NewTable().SetBorders(false).SetFixed(1, 2)
	funcSelect.SetSelectable(true, false).SetBackgroundColor(tcell.ColorBlack)
	funcSelect.SetCell(0, 0, tview.NewTableCell("raw").SetAlign(tview.AlignCenter))
	funcSelect.SetCell(0, 1, tview.NewTableCell("log").SetAlign(tview.AlignCenter))
	funcSelect.SetCell(0, 2, tview.NewTableCell("txn").SetAlign(tview.AlignCenter))
	funcSelect.SetCell(0, 3, tview.NewTableCell("ans").SetAlign(tview.AlignCenter))
	funcSelect.SetCell(0, 4, tview.NewTableCell("bm").SetAlign(tview.AlignCenter))
	funcSelect.Select(0, 0)

	// rootFlex
	rootFlex := tview.NewFlex()
	rootFlex.SetDirection(tview.FlexRow)
	rootFlex.AddItem(contentPages, 0, 1, false).AddItem(funcSelect, 1, 0, true)

	// rawMsgView
	rawMsgView := tview.NewTextView().SetDynamicColors(true)
	rawMsgView.SetTitle("raw message view").SetBorder(true)
	contentPages.AddPage("raw", rawMsgView, true, true)

	// programLogView
	programLogView := tview.NewTextView().SetDynamicColors(true)
	programLogView.SetTitle("program log view").SetBorder(true)
	contentPages.AddPage("log", programLogView, true, true)
	log.SetOutput(programLogView)

	// transaction view
	transactionView := tview.NewFlex()
	transactionViewTable := tview.NewTable().SetBorders(true)
	transactionView.AddItem(transactionViewTable, 0, 1, true)
	contentPages.AddPage("txn", transactionView, true, true)
	transactionViewTable.Select(0, 0)

	// programLogView
	analysisView := tview.NewTextView().SetDynamicColors(true)
	analysisView.SetTitle("analysis view").SetBorder(true)
	contentPages.AddPage("ans", analysisView, true, true)
	log.SetOutput(programLogView)

	// benchmarkView
	benchmarkView := tview.NewTextView().SetDynamicColors(true)
	benchmarkView.SetTitle("benchmark view").SetBorder(true)
	contentPages.AddPage("bm", benchmarkView, true, true)

	// accumulate transaction and making snapshot logic
	transactionLogFeed := make(chan *TransactionPathLog, 100000)
	transactionPathUiBuffer := [][]string{
		{"", "txid", "total elapsed", "most longest step", "logged step count"},
	}
	totalProcessedTransactionLog := 0
	analysisUiBuffer := ""

	// transaction feed processor
	go func() {
		transactionStepsByTxid := map[string]map[string]time.Time{}
		transactionRowByTxid := map[string]int{}
		monotonicRowNum := 1

		for {
			transactionStepAnalysis := map[string]int{}
			transactionStepCountAnalysis := make([]int, 10)
			rowUpdated := 0

			for {
				if len(transactionLogFeed) <= 0 {
					break
				}
				v := <-transactionLogFeed
				totalProcessedTransactionLog += 1
				txid := hex.EncodeToString([]byte(v.TransactionHash))

				if _, exist := transactionStepsByTxid[txid]; !exist {
					transactionStepsByTxid[txid] = map[string]time.Time{}
				}
				transactionStepsByTxid[txid][v.Step] = v.Timestamp
				rowUpdated += 1
			}

			//log.Printf("transactionStepsByTxid: %d, transactionPathUiBuffer: %d", len(transactionStepsByTxid), len(transactionPathUiBuffer))
			if rowUpdated < 1 {
				continue
			}
			timeGapInfo := AnalyzeTimeGaps(transactionStepsByTxid)
			for txid, steps := range transactionStepsByTxid {
				if _, idExist := transactionRowByTxid[txid]; !idExist {
					transactionRowByTxid[txid] = monotonicRowNum // 1
					transactionPathUiBuffer = append(transactionPathUiBuffer, []string{"", "", "", "", ""})
					monotonicRowNum += 1
				}

				transactionPathUiBuffer[transactionRowByTxid[txid]][0] = fmt.Sprintf("%d", transactionRowByTxid[txid])
				transactionPathUiBuffer[transactionRowByTxid[txid]][1] = txid

				totalElapsed := transactionStepsByTxid[txid][timeGapInfo[txid].OverallLatestKey].Sub(transactionStepsByTxid[txid][timeGapInfo[txid].OverallOldestKey])
				transactionPathUiBuffer[transactionRowByTxid[txid]][2] = totalElapsed.String()

				longestStepName := fmt.Sprintf("%s ~ %s", timeGapInfo[txid].LargestGapPreviousKey, timeGapInfo[txid].LargestGapLatestKey)
				longestStepElapsed := transactionStepsByTxid[txid][timeGapInfo[txid].LargestGapLatestKey].Sub(transactionStepsByTxid[txid][timeGapInfo[txid].LargestGapPreviousKey])
				transactionPathUiBuffer[transactionRowByTxid[txid]][3] = fmt.Sprintf("%s (%s)", longestStepName, longestStepElapsed)
				transactionPathUiBuffer[transactionRowByTxid[txid]][4] = fmt.Sprintf("%d", len(steps))

				transactionStepAnalysis[longestStepName] += 1
				transactionStepCountAnalysis[len(steps)] += 1
			}

			buf := ""
			buf += fmt.Sprintf("[white]totally [red]%d [white]transaction path log\n", totalProcessedTransactionLog)
			buf += fmt.Sprintf("[white]totally [red]%d [white]transactions\n\n", len(transactionRowByTxid))
			buf += "[purple]transaction longest step statistic\n"
			keys := lo.Keys(transactionStepAnalysis)
			sort.Strings(keys)
			for _, k := range keys {
				buf += fmt.Sprintf("[green]%s: [red]%d\n", k, transactionStepAnalysis[k])
			}

			buf += "\n[purple]transaction step count statistic\n"
			for i, count := range transactionStepCountAnalysis {
				buf += fmt.Sprintf("[green]%d steps = [red]%d\n", i, count)
			}
			analysisUiBuffer = buf
		}
	}()

	// benchmark ui log
	benchmarkLogFeed := make(chan *BenchmarkLog, 100000)
	benchmarkUiBuffer := ""

	// benchmark feed processor
	go func() {
		benchmarkByName := map[string][]float64{} // value is nano seconds

		for {
			rowUpdated := 0
			for {
				if len(benchmarkLogFeed) <= 0 {
					break
				}
				v := <-benchmarkLogFeed
				if _, exist := benchmarkByName[v.Name]; !exist {
					benchmarkByName[v.Name] = []float64{}
				}
				benchmarkByName[v.Name] = append(benchmarkByName[v.Name], v.Duration)
				rowUpdated += 1
			}

			if rowUpdated < 1 {
				continue
			}
			benchmarkUiBuffer = ""
			for name, list := range benchmarkByName {
				slices.Sort(list)
				benchmarkUiBuffer += fmt.Sprintf("[purple]%s [white]N=[red]%d [white]mean=[red]%f [white]var=[red]%f [white]p99=[red]%f\n", name, len(list), stat.Mean(list, nil), stat.Variance(list, nil), stat.Quantile(0.99, stat.Empirical, list, nil))
			}
		}
	}()

	// UI draw goroutine
	go func() {
		for range time.Tick(1000 * time.Millisecond) {
			for r, row := range transactionPathUiBuffer {
				for c, col := range row {
					transactionViewTable.SetCell(r, c, tview.NewTableCell(col))
				}
			}

			analysisView.SetText(analysisUiBuffer)
			benchmarkView.SetText(benchmarkUiBuffer)

			rawMsgView.ScrollToEnd()
			programLogView.ScrollToEnd()
			app.Draw()
		}
	}()

	nctx, err := setupNATS(func(msg *nats.Msg, ackError error) {
		// check ackError
		if ackError != nil {
			log.Printf("ack error: %s", ackError)
			return
		}

		// parse JSON data
		lw := LogWrapper{}
		if err := json.Unmarshal(msg.Data, &lw); err != nil {
			log.Printf("message: %s", string(msg.Data))
			log.Printf("message unmarshal error: %s", err)
			return
		}

		// verify log type and payload

		switch v := lw.Log.(type) {
		case *TransactionPathLog:
			transactionLogFeed <- v
			//fmt.Fprintf(rawMsgView, "[green]transaction path [white]node[red]%s [white]shard[red]%s [white]txid[red]%s [white]step[red]%s\n", v.NodeId, v.ShardId, txid, v.Step)
		case *BootingSequenceLog:
			fmt.Fprintf(rawMsgView, "[purple]%s ", time.Now().Format(time.TimeOnly))
			fmt.Fprintf(rawMsgView, "[green]booting sequence [white]node[red]%s [white]shard[red]%s [white]sequence=[red]%d\n", v.NodeId, v.ShardId, v.SequenceNumber)
		case *BenchmarkLog:
			benchmarkLogFeed <- v
			fmt.Fprintf(rawMsgView, "[purple]%s ", time.Now().Format(time.TimeOnly))
			fmt.Fprintf(rawMsgView, "[green]benchmark [white]node[red]%s [white]shard[red]%s [white]%s=[red]%f\n", v.NodeId, v.ShardId, v.Name, v.Duration)
		default:
			fmt.Fprintf(rawMsgView, "[purple]%s ", time.Now().Format(time.TimeOnly))
			fmt.Fprintf(rawMsgView, "[green]unknown [white]type[red]%s\n", lw.Type)
		}
	})
	if err != nil {
		log.Panic(err)
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r, c := funcSelect.GetSelection()
		nc := c

		switch event.Key() {
		case tcell.KeyF3: // prev
			if c > 0 {
				nc -= 1
			}
		case tcell.KeyF4: // next
			if c < funcSelect.GetColumnCount()-1 {
				nc += 1
			}
		}

		if c != nc {
			funcSelect.GetCell(r, c).SetTextColor(tcell.ColorWhite)
			funcSelect.GetCell(r, nc).SetTextColor(tcell.ColorPurple)
			funcSelect.Select(r, nc)
			contentPages.SwitchToPage(funcSelect.GetCell(r, nc).Text)
		}

		if funcSelect.GetCell(r, nc).Text == "txn" {
			switch event.Key() {
			case tcell.KeyUp:
				app.SetFocus(transactionViewTable)
			case tcell.KeyDown:
				app.SetFocus(funcSelect)
			}

		}

		return event
	})

	if err := app.SetRoot(rootFlex, true).SetFocus(funcSelect).Run(); err != nil {
		log.Fatal(err)
	}

	if err := nctx.Shutdown(); err != nil {
		log.Printf("Error shutting down NATS connection: %v", err)
	}
	log.Println("NATS server shutdown complete.")
}

func (lw *LogWrapper) UnmarshalJSON(data []byte) error {
	var typeId LogTypeIdentifier
	if err := json.Unmarshal(data, &typeId); err != nil {
		return err
	}
	lw.Type = typeId.Type

	switch typeId.Type {
	case "transaction_path":
		lw.Log = &TransactionPathLog{}
		if err := json.Unmarshal(data, lw.Log); err != nil {
			return err
		}
	case "booting_sequence":
		lw.Log = &BootingSequenceLog{}
		if err := json.Unmarshal(data, lw.Log); err != nil {
			return err
		}
	case "benchmark":
		lw.Log = &BenchmarkLog{}
		if err := json.Unmarshal(data, lw.Log); err != nil {
			return err
		}
	default:
		lw.Log = nil
	}

	return nil
}

// TimeAnalysisResult 구조체는 각 ID에 대한 시간 분석 결과를 저장합니다.
type TimeAnalysisResult struct {
	LargestGapLatestKey   string // 가장 큰 시간 간격에서의 최신 키
	LargestGapPreviousKey string // 가장 큰 시간 간격에서의 이전 키
	OverallLatestKey      string // 전체 스텝 중 가장 최신 키
	OverallOldestKey      string // 전체 스텝 중 가장 오래된 키
}

// AnalyzeTimeGaps 함수는 주어진 데이터 구조에서 각 ID별로 시간 분석을 수행합니다.
// 가장 큰 시간 간격을 가지는 키 쌍과 전체에서 가장 최신/오래된 키를 찾습니다.
// Input: map[string]map[string]time.Time
//
//	(outer key: id, inner map: step -> timestamp)
//
// Output: map[string]TimeAnalysisResult
//
//	(key: id, value: TimeAnalysisResult)
func AnalyzeTimeGaps(data map[string]map[string]time.Time) map[string]TimeAnalysisResult {
	results := make(map[string]TimeAnalysisResult)

	for id, steps := range data {
		if len(steps) < 1 { // 항목이 하나라도 있어야 최신/오래된 키를 찾을 수 있음
			continue
		}

		// 1. Inner map의 entry들을 익명 구조체 슬라이스로 변환
		// Use an anonymous struct directly for local scope.
		entries := make([]struct {
			Key  string
			Time time.Time
		}, 0, len(steps))

		for key, timestamp := range steps {
			// Append using an anonymous struct literal.
			entries = append(entries, struct {
				Key  string
				Time time.Time
			}{Key: key, Time: timestamp})
		}

		// 2. 시간(time.Time) 기준 내림차순(최신순)으로 정렬
		// Sort the slice based on the Time field in descending order.
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Time.Equal(entries[j].Time) {
				return entries[i].Key > entries[j].Key
			}
			return entries[i].Time.After(entries[j].Time) // 내림차순 정렬 (Descending sort)
		})

		// 3. 전체에서 가장 최신 및 가장 오래된 키 식별
		// Identify the overall latest and oldest keys after sorting.
		overallLatestKey := entries[0].Key
		overallOldestKey := entries[len(entries)-1].Key

		// 4. 가장 큰 시간 간격 찾기 (최소 2개 항목 필요)
		// Find the largest time gap (requires at least 2 entries).
		var maxDuration time.Duration = -1 // Initialize with a negative value
		largestGapLatestKey := ""
		largestGapPreviousKey := ""
		foundGap := false

		if len(entries) >= 2 {
			for i := 0; i < len(entries)-1; i++ {
				// Calculate the duration between consecutive entries.
				duration := entries[i].Time.Sub(entries[i+1].Time)

				// Update if the current duration is larger than maxDuration,
				// or if maxDuration is still initial value and current duration is non-negative
				// (to select the first gap if all gaps are zero).
				if duration > maxDuration || (maxDuration < 0 && duration >= 0) {
					maxDuration = duration
					largestGapLatestKey = entries[i].Key
					largestGapPreviousKey = entries[i+1].Key
					foundGap = true
				}
			}
		}

		// 5. 결과 저장
		// Store the results, including overall keys even if no gap was found.
		analysisResult := TimeAnalysisResult{
			OverallLatestKey: overallLatestKey,
			OverallOldestKey: overallOldestKey,
		}
		if foundGap {
			analysisResult.LargestGapLatestKey = largestGapLatestKey
			analysisResult.LargestGapPreviousKey = largestGapPreviousKey
		}
		results[id] = analysisResult

	}

	return results
}
