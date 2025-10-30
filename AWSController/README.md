# AWS 콘솔 aws-cli를 통한 스크립트 관리

## !중요사항

- **deploy로 테스트를 한 다음, 반드시 instance를 stop 하거나(기록을 보관하고 싶은경우) terminate 해야 함**

- **가능하면 terminate를 진행하도록 하고, 남겨야 하는 내용은 로컬에 저장하도록 하자**

## 환경

- Ubuntu
- aws-cli

## aws-cli 설치

```
$ cd AWSController
$ ./install_aws_cli.sh

```

### version 확인

```
$ aws --version

> aws-cli/2.7.27 Python/3.9.11 Linux/5.10.16.3-microsoft-standard-WSL2 exe/x86_64.ubuntu.20 prompt/off
```

## configure 설정

### Keypair 설정

- 현재 AWS 사용자 그룹 *Falcon*의 각 사용자가 설정 되있음
  - shhong
  - jwyang
  - hwkim
  - wjlee
  - chchoi
- 각 IAM 사용자로 로그인 하여 문서 지침을 통해 키 생성 ([키 생성 방법](https://docs.aws.amazon.com/ko_kr/cli/latest/userguide/cli-configure-quickstart.html#cli-configure-quickstart-creds-create))
  - 이미 Auth Key 메일로 전송하였으므로, 첨부파일 다운로드하여 사용

### configure

```
$ aws configure
```

**아래 옵션을 알맞게 채워 넣는다, id와 key 경우에는 본인 user에 대한 것으로 설정**

_각자 Auth Key를 사용하여 진행_

- id
- key
- region: ap-northeast-2
- format: json

### +추가 SSH 설정 (deploy 시에 필요한 설정임, 필수 !!!)

- 인스턴스에 대한 SSH KEY 설정

  - 스크립트를 통해 실행된 AWS instance는 모두 같은 KEY로 접속할 수 있게 됨

  - 따라서, 해당 KEY를 Default public key로 설정하고 사용해야 함

1. unishard.pem 다운로드

2. 경로 및 권한 설정 (chmod 500)

3. ~/.ssh/config 생성 후 "IdentityFile [FALCON.PEM 경로]" 추가

4. ~~인스턴스에 대하여 public key fingerprint 설정 (참고 문서 [fingerprint 이슈 해결](https://blueyikim.tistory.com/1792))~~ **_바로 5번 진행_**

- 처음 인스턴스에 접속하는 경우, 다음 접속시 사용할 fingerprint를 남길지 여부를 물음

- 실험시, 모든 인스턴스에 대하여 응답할 수 없으므로 미리 설정해야 함

- (아래 명령어는 get_public_DNS.sh 를 실행하고 나서 해야 함)

```
ssh-keyscan -t ed25519 -f ../bin/deploy/public_ips.txt >> ~/.ssh/known_hosts
```

5. (수정) 위 방법은 항상 정상 작동하지 않는 단점이 있으므로, 아예 fingerprint에 대해 묻지 않고 바로 접속하게 하도록 config 설정 진행

- 3번에서 생성한 파일에 아래 내용 추가

```
IdentityFile [FALCON.PEM 경로]

# Add No Host Checking
Host *
        StrictHostKeyChecking no
```

## 샤드와 커미티 개수 조절하기
```
$ cd bin/deploy
$ vim config.conf
shard와 committee수정하기  *파일의 맨 아래 빈 한줄은 없애면 안됨
```

## 인스턴스 생성할때
- 뭄바이, 파리, 스톡홀름은 C4계열의 인스턴스를 지원하지 않으므로 이 3개의 리전을 포함시키게 되면 C52XL로 인스턴스유형을 변경해서 사용해야함
```
$ cd AWSController
$ ./run_instance.sh
$ ./get_IP.sh
```

## 인스턴스 시작할때
```
$ cd AWSController
$ ./start_instance.sh
```

## 인스턴스 종료할때
```
$ cd AWSController
$ ./stop_instance.sh
```

## 시작된 인스턴스의 IP를 가져올때 > base_ips.txt와 ips.txt에 채워짐
```
$ cd AWSController
$ ./get_IP.sh
```

## 인스턴스를 지울때
```
$ cd AWSController
$ ./terminate_instance.sh
```

## 인스턴스에 파일들 deploy시키기 > 맨 마지막 IP의 인스턴스에 접속해서 다 전송되었는지 확인해야함
```
$ cd bin/deploy
$ ./deploy.sh
```

## 인스턴스에 변경된 파일을 전송할때
```
$ cd bin/deploy
보낼 파일들로 해당 명령어 추가/삭제 수정하기 * 만약에 코드를 수정했다면 build 다시 하고 server실행파일 보낼것
$ ./update_conf.sh
```

## 인스턴스에서 프로토콜 시작하기
```
$ cd bin/deploy
$ ./start.sh
```

## 인스턴스에서 프로토콜 종료하기
```
$ cd bin/deploy
$ ./stop.sh
```

## 인스턴스에서 로그 가져오기
## 맨 마지막 IP의 인스턴스에 접속해서 ps-ef | grep server 명령어로 다 옮겨졌는지 확인해야함 프로세스가 없으면 다 옮겨진것임
```
$ cd bin/deploy
$ ./getLogs.sh
```