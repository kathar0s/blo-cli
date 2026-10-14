---
name: blo
description: 빌링온(Billing-On) AWS 비용을 조회·분석할 때 사용. `blo ...` CLI로 월별/일별 총비용, 서비스별·계정별 비용 분해, 전월 대비 증감, CSV/JSON export를 수행한다. 사용자가 AWS 비용/요금/빌링온/클라우드 지출을 묻거나 비용 리포트를 요청할 때 로드한다.
---

# Billing-On CLI (`blo`)

`blo` 는 빌링온 포털의 AWS 비용 데이터를 명령줄에서 조회하는 도구다.
빌링온은 공개 API가 없어 내부 AJAX 프로토콜을 재현하므로, 항상 `blo` 를 통해 접근한다.

## TLDR

1. 인증이 안 돼 있으면 먼저 `blo auth login` (또는 `BILLINGON_EMAIL`/`BILLINGON_PASSWORD` 환경변수).
2. 데이터는 `--json` 으로 받아 `jq` 로 가공한다.

```bash
blo auth status                 # 로그인 상태 확인
blo cost summary --json         # 당월/전월/예상 총비용
blo cost account -m 2026-04 --json
```

## 명령 탐색

명령 구조는 `--help` 로 동적으로 확인한다 (하드코딩하지 말 것).

```bash
blo --help
blo cost --help
blo cost account --help
```

## 인증

- `blo auth status` 로 먼저 확인. 미로그인이면 `blo auth login`.
- 자동화/스크립트에서는 환경변수: `BILLINGON_EMAIL`, `BILLINGON_PASSWORD`.
- ⚠️ **단일 세션 제약**: 빌링온은 계정당 세션 1개만 허용한다. `blo auth login` 을
  실행하면 사용자가 열어둔 웹 브라우저 세션이 끊긴다. 꼭 필요할 때만 로그인하고,
  이미 세션이 있으면(`blo auth status` 가 로그인됨) 재로그인하지 말 것.
- 세션은 `~/.config/billing-on/credentials.json` 에 저장된다(비밀번호는 저장 안 함).
- `세션 만료` 오류가 나면 `blo auth login` 을 다시 실행한다.

## 출력 형식

| 플래그 | 용도 |
|---|---|
| (기본) | 사람이 읽기 좋은 표 |
| `--json` | 프로그램 가공/`jq` 파이프용 |
| `--csv` | 스프레드시트/리포트용 |

## 캐싱

과거 월 조회 결과는 자동 캐시된다(이번 달=캐시 안 함, 지난달=24h, 2달 전 이상=영구).
같은 과거 월을 반복 조회해도 서버를 다시 치지 않으니 자유롭게 호출해도 된다. 최신
값이 꼭 필요하면 `--refresh` 를 붙인다. `blo cache info` / `blo cache clear` 로 관리.

## 핵심 명령

```bash
blo cost summary [-m YYYY-MM]          # 총비용 요약 + 전월 대비 증감률 (사용액)
blo cost monthly [-m YYYY-MM]          # 월별 추이
blo cost service [-m YYYY-MM] [--top N] # 서비스(Product)별 분해 (사용액)
blo cost account [-m YYYY-MM]          # AWS 계정별 분해 (사용액)
blo cost invoice [-m YYYY-MM] [--account N] [--by-account]  # 실제 청구액(계정×서비스)
blo cost export -m YYYY-MM [-o file]   # 전체 사용 항목 CSV (상세 분석용)
blo groups                             # 그룹 목록 (-g 로 특정 그룹 지정 가능)
blo months                             # 데이터가 있는 월 목록
```

## 비용 기준 2가지 (반드시 구분)

빌링온은 두 가지 다른 금액 기준이 있다. 질문에 맞는 명령을 골라야 한다.

- **사용액** (그 달 실제 사용분): `cost summary`, `cost account`, `cost service`(필터 없음), `cost export`.
  "이번 달 얼마나 썼나", 서비스/계정 비중 분석에 사용.
- **실제 청구액** (인보이스 발행액, 전월 사용분 + 약정 포함): `cost invoice`,
  `cost service --account`. "실제로 청구된 금액이 얼마인가"에는 **반드시 `cost invoice`** 를 쓴다.

두 기준은 금액이 다르다(예: 2026-05 사용액 ~$20.6k vs 청구액 ~$25.3k). 섞어 비교하지 말 것.
`cost invoice --by-account` 로 계정별 청구 합계, `cost invoice --by-service` 로 서비스별
청구 합계, `cost invoice --account <no>` 로 특정 계정의 서비스별 청구내역을 본다.
출력 하단에 제품 합계(할인 전)·할인·실청구액(net)이 3단으로 표시된다.

- `-m, --month YYYY-MM` 생략 시 이번 달.
- `-g, --group <id>` 생략 시 통합 그룹 자동 선택. 그룹 ID는 `blo groups` 로 확인.

## 상세 분석은 `cost export` 를 쓴다 (중요)

리소스/사용량 단위의 깊은 분석(예: "어떤 리소스가 비싼가", "특정 서비스의 일자별
추이", 임의 그룹핑·필터·집계)이 필요하면, 여러 번 조회하지 말고 **`cost export` 로
그 달 전체 라인아이템 CSV를 한 번에 받아서** 직접 분석한다.

```bash
blo cost export -m 2026-01 -o /tmp/2026-01.csv   # 파일로 저장 후 읽어서 분석
```

- 컬럼: `번호,날짜,그룹,Account ID,Operation,ResourceId,ProductName,UsageType,Region,ItemDescription,UsageQuantity,PricingUnit,금액(USD)` (UTF-8).
- 빌링온은 데이터를 그룹별로 보관(과거: 계정별 분리, 현재: 통합 그룹)하지만, `export`
  는 모든 그룹을 자동 병합하므로 월만 지정하면 전체가 나온다.
- 한 달이 수십만 행일 수 있으니, stdout 파이프보다 `-o` 로 **파일 저장 후 읽는 것**을 권장.

## 일반 패턴

```bash
# 이번 달 가장 비싼 서비스 5개
blo cost service --top 5

# 계정별 비용을 CSV로 저장
blo cost account -m 2026-05 --csv > accounts-2026-05.csv

# 전월 대비 증감률만 추출
blo cost summary --json | jq '.currentCost, .previousCost'

# 리소스 단위 상세 분석: 전체를 받아서 직접 집계
blo cost export -m 2026-05 -o /tmp/may.csv   # 이후 CSV를 읽어 분석
```

## AWS CLI 연계: "왜 비싼가" 원인 분석

`blo` 는 **무엇이/얼마나** 비싼지(비용)를 알려주고, **왜** 비싼지(사용량 원인)는
`aws` CLI로 파고든다. 빌링온을 쓰는 환경은 보통 SCP로 Cost Explorer(`aws ce ...`)가
막혀 있지만(`explicit deny`), **CloudWatch·CloudTrail·각 서비스 describe API는 살아 있어**
원인 추적이 가능하다.

분석 흐름 (2단계):

1. **`blo` 로 비싼 대상 특정** — `cost export` CSV의 `ProductName`/`UsageType`/`Account ID`
   /`ResourceId` 컬럼으로 어떤 서비스의 어떤 사용유형이 비용을 끌어올렸는지 집계.
   `UsageType` 이 원인 유형을 직접 가리킨다 (예: `CW:MetricStorage`=커스텀 메트릭,
   `CW:Requests`=API 호출, `BoxUsage:*`=EC2 인스턴스 시간, `*-TimedStorage-*`=S3 스토리지).

   ```bash
   blo cost export -m 2026-05 -o /tmp/may.csv
   # 특정 계정에서 CloudWatch 비용을 UsageType별로 집계 (4=AccountID,8=UsageType,13=금액)
   awk -F, 'NR>1 && $7 ~ /CloudWatch/ {gsub(/"/,"",$13); a[$8]+=$13}
            END{for(k in a) printf "%.2f\t%s\n",a[k],k}' /tmp/may.csv | sort -rn
   ```

2. **`aws` CLI 로 원인 검증** — 1단계의 UsageType에 맞춰 실제 리소스/사용량 추적.
   ⚠️ `aws` 는 **현재 인증된 계정**만 접근 가능. 다른 계정은 해당 계정 자격증명/역할 필요.

   ```bash
   # CW:MetricStorage 가 비싸다 → 커스텀 메트릭 개수 폭증 의심
   aws cloudwatch list-metrics --query 'length(Metrics)'
   # CW:Requests 가 비싸다 → 누가 자주 호출하는지 (CloudTrail)
   aws cloudtrail lookup-events \
     --lookup-attributes AttributeKey=EventSource,AttributeValue=monitoring.amazonaws.com
   # EC2 BoxUsage 가 비싸다 → 실행 중 인스턴스 타입/개수
   aws ec2 describe-instances \
     --query 'Reservations[].Instances[].[InstanceId,InstanceType,State.Name]' --output table
   ```

원칙: 비용 수치는 `blo`(빌링온이 유일 소스), 사용량/구성 원인은 `aws`. 둘을 엮어
"이 계정의 이 서비스가, 이 UsageType 때문에, 이 리소스에서 비싸졌다"까지 좁힌다.
단 `ResourceId` 가 `Not Applicable`(Savings Plans 등)이면 리소스 단위 추적은 불가.

## 주의

빌링온 서버 사양이 바뀌면 `blo` 가 깨질 수 있다. 명령이 실패하면
`blo` 버전을 최신으로 업데이트(`blo --version` 확인)하거나 이슈를 제보한다.
