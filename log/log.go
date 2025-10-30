package log

import (
	"fmt"
	"io"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Severity int32

const (
	DEBUG Severity = iota
	INFO
	WARNING
	ERROR
	CHANGE_TEST
)

var names = []string{
	DEBUG:       "DEBUG",
	INFO:        "INFO",
	WARNING:     "WARNING",
	ERROR:       "ERROR",
	CHANGE_TEST: "CHANGE_TEST",
}

type Type byte

func (s *Severity) Get() interface{} {
	return *s
}

func (s *Severity) Set(value string) error {
	threshold := INFO
	value = strings.ToUpper(value)
	for i, name := range names {
		if name == value {
			threshold = Severity(i)
		}
	}
	*s = threshold
	return nil
}

func (s *Severity) String() string {
	return names[int(*s)]
}

type logger struct {
	sync.Mutex
	// buffer *buffer

	// *stdlog.Logger
	gdebug           *stdlog.Logger
	ginfo            *stdlog.Logger
	cdebug           *stdlog.Logger
	cinfo            *stdlog.Logger
	debug            *stdlog.Logger
	info             *stdlog.Logger
	warning          *stdlog.Logger
	err              *stdlog.Logger
	shardStatistic   *stdlog.Logger
	experimentResult *stdlog.Logger
	// shardLoggers     []*stdlog.Logger
	changeTestLogger *stdlog.Logger

	Severity Severity
	dir      string
}

var log logger

// Setup setup log format and output file
func Setup() {
	format := stdlog.Ldate | stdlog.Ltime | stdlog.Lmicroseconds | stdlog.Lshortfile
	fGname := fmt.Sprintf("%s.%d.gateway.log", filepath.Base(os.Args[0]), os.Getpid())
	fg, err := os.Create(filepath.Join(log.dir, fGname))
	if err != nil {
		stdlog.Fatal(err)
	}
	fCname := fmt.Sprintf("%s.%d.coordination.log", filepath.Base(os.Args[0]), os.Getpid())
	fc, err := os.Create(filepath.Join(log.dir, fCname))
	if err != nil {
		stdlog.Fatal(err)
	}

	// 모든 샤드의 로그를 main.log에 작성하는 것이 아닌 각 샤드의 로그 파일에 작성(e.g., main.shard1.log)
	// shardNum := config.GetConfig().ShardCount
	// log.shardLoggers = make([]*stdlog.Logger, shardNum)
	// for i := 0; i < shardNum; i++ {
	// 	fnameshard := fmt.Sprintf("%s.%d.shard%d.log", filepath.Base(os.Args[0]), os.Getpid(), i)
	// 	fShard, err := os.Create(filepath.Join(log.dir, fnameshard))
	// 	if err != nil {
	// 		stdlog.Fatal(err)
	// 	}
	// 	log.shardLoggers[i] = stdlog.New(fShard, fmt.Sprintf("[SHARD%v] ", i), format)
	// }

	fName := fmt.Sprintf("%s.%d.log", filepath.Base(os.Args[0]), os.Getpid())
	f, err := os.Create(filepath.Join(log.dir, fName))
	if err != nil {
		stdlog.Fatal(err)
	}
	fnameShardStatistic := fmt.Sprintf("%s.%d.shardStatistic.log", filepath.Base(os.Args[0]), os.Getpid())
	fShardStatistic, err := os.Create(filepath.Join(log.dir, fnameShardStatistic))
	if err != nil {
		stdlog.Fatal(err)
	}
	fnameExperimentResult := fmt.Sprintf("%s.%d.experimentResult.csv", filepath.Base(os.Args[0]), os.Getpid())
	fExperimentResult, err := os.Create(filepath.Join(log.dir, fnameExperimentResult))
	if err != nil {
		stdlog.Fatal(err)
	}
	fnameChangeTestResult := fmt.Sprintf("%s.%d.changeTest.log", filepath.Base(os.Args[0]), os.Getpid())
	fChangeTestResult, err := os.Create(filepath.Join(log.dir, fnameChangeTestResult))
	if err != nil {
		stdlog.Fatal(err)
	}

	log.gdebug = stdlog.New(fg, "[GDEBUG] ", format)
	log.ginfo = stdlog.New(fg, "[GINFO] ", format)
	log.cdebug = stdlog.New(fc, "[CDEBUG] ", format)
	log.cinfo = stdlog.New(fc, "[CINFO] ", format)
	log.debug = stdlog.New(f, "[DEBUG] ", format)
	log.info = stdlog.New(f, "[INFO] ", format)
	multi := io.MultiWriter(f, os.Stderr)
	log.warning = stdlog.New(multi, "[WARNING] ", format)
	log.err = stdlog.New(multi, "[ERROR] ", format)
	log.shardStatistic = stdlog.New(fShardStatistic, "[SHARD1STATISTIC]", format)
	log.experimentResult = stdlog.New(fExperimentResult, "", 0)
	log.changeTestLogger = stdlog.New(fChangeTestResult, "[CHANGE_TEST] ", format)
}

func GDebug(v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.gdebug.Output(2, fmt.Sprint(v...))
	}
}

func GDebugf(format string, v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.gdebug.Output(2, fmt.Sprintf(format, v...))
	}
}

func GInfo(v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.ginfo.Output(2, fmt.Sprint(v...))
	}
}

func GInfof(format string, v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.ginfo.Output(2, fmt.Sprintf(format, v...))
	}
}

func CDebug(v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.cdebug.Output(2, fmt.Sprint(v...))
	}
}

func CDebugf(format string, v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.cdebug.Output(2, fmt.Sprintf(format, v...))
	}
}

func CInfo(v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.cinfo.Output(2, fmt.Sprint(v...))
	}
}

func CInfof(format string, v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.cinfo.Output(2, fmt.Sprintf(format, v...))
	}
}

// func ShardLoggers() []*stdlog.Logger {
// 	return log.shardLoggers
// }

func Debug(v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.debug.Output(2, fmt.Sprint(v...))
	}
}

func Debugf(format string, v ...interface{}) {
	if log.Severity == DEBUG {
		_ = log.debug.Output(2, fmt.Sprintf(format, v...))
	}
}

func Info(v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.info.Output(2, fmt.Sprint(v...))
	}
}

func Infof(format string, v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.info.Output(2, fmt.Sprintf(format, v...))
	}
}

func ShardStatisticf(format string, v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.shardStatistic.Output(2, fmt.Sprintf(format, v...))
	}
}

func ExperimentResult(v ...interface{}) {
	_ = log.experimentResult.Output(2, fmt.Sprint(v...))
}

func ExperimentResultf(format string, v ...interface{}) {
	_ = log.experimentResult.Output(2, fmt.Sprintf(format, v...))
}

func ChangeTest(v ...interface{}) {
	_ = log.changeTestLogger.Output(2, fmt.Sprint(v...))
}

func ChangeTestf(format string, v ...interface{}) {
	_ = log.changeTestLogger.Output(2, fmt.Sprintf(format, v...))
}

func Warning(v ...interface{}) {
	if log.Severity <= WARNING {
		_ = log.warning.Output(2, fmt.Sprint(v...))
	}
}

func Warningf(format string, v ...interface{}) {
	if log.Severity <= WARNING {
		_ = log.warning.Output(2, fmt.Sprintf(format, v...))
	}
}

func Error(v ...interface{}) {
	_ = log.err.Output(2, fmt.Sprint(v...))
}

func Errorf(format string, v ...interface{}) {
	_ = log.err.Output(2, fmt.Sprintf(format, v...))
}

func Fatal(v ...interface{}) {
	_ = log.err.Output(2, fmt.Sprint(v...))
	stdlog.Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	_ = log.err.Output(2, fmt.Sprintf(format, v...))
	stdlog.Fatalf(format, v...)
}

func Testf(format string, v ...interface{}) {
	if log.Severity <= INFO {
		_ = log.info.Output(3, fmt.Sprintf(format, v...))
	}
}

type TestLog[T any] struct {
	Title       string
	Description string
	Expected    T
	Actual      T
	Error       error
}

// setting the case is normal or abnormal
func (tl *TestLog[T]) IsNormal(normal bool) *TestLog[T] {
	if normal {
		tl.Title = "Normal Case"
	} else {
		tl.Title = "Abnormal Case"
	}

	return tl
}

// set description
// then, return its instance to be used for print
func (tl *TestLog[T]) SetCaseDesc(description string) *TestLog[T] {
	tl.Description = description

	return tl
}

// print what this testcase is (title and description)
func (tl *TestLog[T]) PrintCase() {
	Testf("%-15v - %v", tl.Title, tl.Description)
}

// print what this testcase is (title and description)
func (tl *TestLog[T]) PrintRawResultFor(result string) {
	Testf("%-15v - %v", "CORRECT", result)
}

// set values for expected and actual
// then, return its instance to be used for print
func (tl *TestLog[T]) SetValues(expected T, actual T) *TestLog[T] {
	tl.Expected = expected
	tl.Actual = actual
	return tl
}

// print expected and actual values for some variable
func (tl *TestLog[T]) PrintResultFor(object string) {
	Testf("%-15v - expected: %-5v | actual: %-5v values are same for %v", "CORRECT", tl.Expected, tl.Actual, object)
}

// print expected and actual values are same or not
func (tl *TestLog[T]) PrintIsEqual(result string) {
	Testf("%-15v - values are same: %v", "CORRECT", result)
}

// // print expected and actual values are different or not
func (tl *TestLog[T]) PrintIsNotEqual(result string) {
	Testf("%-15v - values are different: %v", "CORRECT", result)
}

// set error
// then, return its instance to be used for print
func (tl *TestLog[T]) SetError(err error) *TestLog[T] {
	tl.Error = err
	return tl
}

// print error is existed
func (tl *TestLog[T]) PrintIsError() {
	Testf("%-15v - error is occurred: %v", "CORRECT", tl.Error)
}

// print error is not existed
func (tl *TestLog[T]) PrintIsNoError() {
	Testf("%-15v - error is not occurred", "CORRECT")
}
