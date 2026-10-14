# blo — Billing-On CLI

[![CI](https://github.com/kathar0s/blo-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/kathar0s/blo-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kathar0s/blo-cli)](https://github.com/kathar0s/blo-cli/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

빌링온(Billing-On, ㈜이테크시스템 AWS MSP 비용관리 포털)의 데이터를 명령줄에서
조회·분석하고 CSV/JSON으로 내보내는 CLI.

AWS SCP로 Cost Explorer가 막힌 환경에서 빌링온 웹 화면을 일일이 보지 않고
비용을 빠르게 조회·분석하기 위해 만들었습니다.

```console
$ blo cost summary
항목       기간                       비용(USD)
---------  -------------------------  ----------
당월       2026-05-01 ~ 2026-05-29     1,234.56
전월       2026-04-01 ~ 2026-05-01     1,500.00
당월 예상  2026-05                     1,320.00

전월 대비: -17.7%
```

> ⚠️ 빌링온은 공개 API가 없어, 웹 SPA가 쓰는 내부 AJAX 프로토콜(AES 암호화 요청 +
> RSA 로그인)을 재현합니다. 서버 사양이 바뀌면 동작이 깨질 수 있습니다.
> 자세한 구현은 `internal/` 패키지 주석을 참고하세요.
>
> ⚠️ **단일 세션 제약**: 빌링온은 계정당 하나의 세션만 허용합니다. `blo` 로 로그인하면
> 같은 계정으로 열려 있던 웹 브라우저 세션이 끊기고, 반대도 마찬가지입니다.
> CLI와 웹을 동시에 쓸 수 없으니 번갈아 사용하세요.

## 설치

### 1) 설치 스크립트 (macOS / Linux, 권장)

최신 릴리스 바이너리를 `~/.local/bin` 에 설치합니다.

```bash
curl -fsSL https://raw.githubusercontent.com/kathar0s/blo-cli/main/install.sh | bash
```

### 2) Homebrew (macOS / Linux)

```bash
brew install kathar0s/tap/blo
```

### 3) go install (Go 1.25+)

```bash
go install github.com/kathar0s/blo-cli@latest
# 바이너리는 $(go env GOPATH)/bin/blo-cli 로 설치됩니다. blo 로 쓰려면:
#   ln -sf "$(go env GOPATH)/bin/blo-cli" "$(go env GOPATH)/bin/blo"
```

### 4) 사전 빌드 바이너리 직접 다운로드

[Releases](https://github.com/kathar0s/blo-cli/releases) 페이지에서 OS/아키텍처에
맞는 아카이브를 받아 압축을 풀고 `blo` 를 PATH에 둡니다.

### 5) 소스 빌드

```bash
git clone https://github.com/kathar0s/blo-cli.git
cd blo-cli
go build -o blo .
```

## 사용법

### 로그인

```bash
# 대화형 (비밀번호는 화면에 표시되지 않고, 저장되지도 않습니다)
blo auth login --email you@example.com

# 비대화형 (CI/자동화) — 환경변수
# ⚠️ blo 는 stdin 이 터미널(TTY)이면 env 가 있어도 비번을 묻습니다. env 비번은
#    stdin 이 TTY 가 아닐 때만 쓰이므로, 대화형 셸에선 </dev/null 을 붙이세요.
#    (CI 는 보통 TTY 가 없어 그냥 동작합니다.)
export BILLINGON_EMAIL=you@example.com
export BILLINGON_PASSWORD='...'
blo auth login </dev/null
```

로그인하면 **비밀번호가 아니라 발급된 세션 쿠키**만
`~/.config/billing-on/credentials.json`(권한 600)에 저장됩니다.

```bash
blo auth status   # 로그인 상태 확인
blo auth logout   # 세션 삭제
```

### 인증 자동화 (시크릿 저장 · 자동 재로그인)

매번 로그인하지 않고, 세션이 끊겨도 자동 복구되게 하는 실전 설정입니다. 비밀번호를
안전하게 저장해 `BILLINGON_PASSWORD` 로 로드하고, 세션 만료 시 자동 재로그인하는
래퍼를 셸 rc 에 둡니다.

**1) 비밀번호 저장 — 환경에 맞게**

- **SSH·헤드리스 (권장)** — macOS login 키체인은 GUI 로그인 때만 잠금해제되어 SSH
  세션에선 잠긴 채라, 키체인 접근이 실패합니다(`security: … User interaction is not
  allowed`, `-w` 읽기는 빈 값). 그래서 `~/.aws/credentials` 처럼 **권한 잠근 평문 파일**이
  정석입니다.

  ```bash
  mkdir -p ~/.config/billing-on
  ( umask 077; printf '%s' 'your-password' > ~/.config/billing-on/password )   # 파일 권한 600
  ```

- **macOS 데스크톱 (GUI 터미널 전용)** — GUI 세션은 키체인이 자동 잠금해제되므로 키체인도 가능:

  ```bash
  security add-generic-password -a you@example.com -s billingon -w   # 프롬프트에 비번 입력
  ```

**2) 셸 rc 에 로더 + 자동 재로그인 래퍼** (`~/.zshrc` 또는 `~/.bashrc`, bash·zsh 공용)

```bash
export BILLINGON_EMAIL="you@example.com"
export BILLINGON_PASSWORD="$(cat ~/.config/billing-on/password 2>/dev/null)"
# 키체인을 쓴다면 위 대신:
# export BILLINGON_PASSWORD="$(security find-generic-password -a "$BILLINGON_EMAIL" -s billingon -w 2>/dev/null)"

# 세션 만료 시 자동 재로그인 후 1회 재시도 (stdout 은 그대로 통과 → 파이프/jq 안전)
blo() {
  local err rc
  { err="$(command blo "$@" 2>&1 1>&3 3>&-)"; rc=$?; } 3>&1
  if [ "$rc" -ne 0 ] && printf '%s' "$err" | grep -q 'session expired'; then
    command blo auth login --email "$BILLINGON_EMAIL" </dev/null >/dev/null 2>&1
    command blo "$@"
  else
    [ -n "$err" ] && printf '%s\n' "$err" >&2
    return "$rc"
  fi
}
```

적용: `source ~/.zshrc` 후 최초 1회만 `blo auth login </dev/null`. 이후 세션이 끊겨도
다음 `blo` 호출이 조용히 재로그인하고 재실행합니다.

**3) 단일 세션 제약 회피** — CLI 로그인은 같은 계정의 웹 세션을 끊습니다(반대도 동일).
개인 계정으로 웹을 자주 쓰면서 CLI 도 상시 돌리려면, **CLI 전용 계정**(예: `monitor@…`)을
따로 두고 위 설정을 그 계정으로 하면 서로 충돌하지 않습니다.

### 명령

```bash
blo --help                            # 전체 명령 목록

blo cost summary                      # 당월/전월/예상 총비용 + 증감률
blo cost summary -m 2026-04           # 특정 월
blo cost monthly                      # 월별 추이
blo cost service                      # 서비스(Product)별 분해
blo cost service --top 10             # 상위 10개
blo cost account                      # AWS 계정별 분해 (사용액)
blo cost invoice --by-account         # 실제 청구액 — 계정별 합계
blo cost invoice --account 1234..     # 실제 청구액 — 특정 계정의 서비스별
blo cost export -m 2026-01 -o jan.csv  # 전체 사용 항목 CSV
blo groups                            # 그룹 목록
blo months                            # 데이터가 있는 월 목록
```

> **비용 기준 두 가지**: `summary`/`account`/`service`/`export` 는 **사용액**(그 달 사용분),
> `invoice` 는 **실제 청구액**(인보이스 발행액 = 전월 사용분 + 약정 포함)입니다. 두 값은
> 다를 수 있으니("얼마 썼나"=사용액, "얼마 청구됐나"=`invoice`) 용도에 맞게 쓰세요.

### 전체 상세 내보내기 (`cost export`)

한 달치 **모든 사용 항목**(계정·리소스·리전·사용량·비용)을 UTF-8 CSV로 받습니다.
빌링온은 데이터를 그룹 단위로 보관하는데, **과거에는 계정마다 그룹이 분리**(GROUP1,
GROUP2, …)되어 있고 **최근에는 통합 그룹** 하나에 모여 있습니다. `cost export` 는
`-g` 없이 실행하면 **모든 그룹을 병렬로 받아 하나의 CSV로 병합**하므로, 어느 방식이든
그 달 전체를 한 번에 얻습니다.

```bash
blo cost export -m 2026-05 -o may.csv   # 파일로 저장 (진행 상황은 stderr)
blo cost export -m 2026-01 > jan.csv    # 표준출력으로 (파이프 가능)
```

CSV 헤더: `번호,날짜,그룹,Account ID,Operation,ResourceId,ProductName,UsageType,Region,ItemDescription,UsageQuantity,PricingUnit,금액(USD)`

### "왜 비싼가" — AWS CLI 연계 분석

`blo` 는 **무엇이/얼마나** 비싼지(비용)를 알려주고, **왜** 비싼지(사용량 원인)는
`aws` CLI로 파고듭니다. SCP가 막는 건 Cost Explorer(`aws ce`)뿐이라 CloudWatch·CloudTrail
·각 서비스 describe API는 대개 살아 있습니다. `cost export` CSV의 `UsageType` 컬럼이
원인 유형(예: `CW:MetricStorage`=커스텀 메트릭, `BoxUsage:*`=EC2 인스턴스 시간)을 가리키고,
거기서 `aws` CLI로 실제 리소스를 추적합니다. 자세한 흐름은
[skills/blo/SKILL.md](./skills/blo/SKILL.md)의 "AWS CLI 연계" 섹션 참고.

### 출력 형식

조회 명령(`summary`/`monthly`/`service`/`account`/`groups`/`months`)에 `--json` / `--csv`
를 붙일 수 있습니다. (`export` 는 항상 원본 CSV를 그대로 내보냅니다.)

```bash
blo cost account -m 2026-05 --csv > accounts.csv
blo cost summary --json | jq .
```

### 옵션

- `-m, --month YYYY-MM` : 조회 월 (기본: 이번 달)
- `-g, --group <id>` : 그룹 ID (기본: 통합 그룹 자동 선택). `blo groups`로 확인.
- `--refresh` : 캐시를 무시하고 다시 조회 (아래 "캐싱" 참고)

### 캐싱

과거 월의 조회 결과는 로컬에 캐시되어 같은 요청을 반복하지 않습니다(빌링온은
세션이 1개라 호출을 아끼는 게 중요합니다). 캐시는 월에 따라 다르게 동작합니다.

- **이번 달**: 매번 바뀌므로 캐시하지 않음 (항상 라이브)
- **지난달**: 인보이스가 확정되는 동안 바뀔 수 있어 24시간만 캐시
- **2달 전 이상**: 불변으로 보고 영구 캐시

`--refresh` 를 붙이면 캐시를 무시하고 다시 조회합니다(새 값으로 캐시도 갱신).
캐시는 OS 캐시 디렉터리(`blo/`)에 계정별로 저장됩니다.

```bash
blo cache info     # 캐시 위치와 사용량
blo cache clear    # 캐시 전체 삭제
blo cost invoice -m 2026-03 --refresh   # 캐시 무시하고 강제 재조회
```

### 셸 자동완성

```bash
# zsh
blo completion zsh > "${fpath[1]}/_blo" && exec zsh
# bash
blo completion bash | sudo tee /etc/bash_completion.d/blo > /dev/null
```

`blo completion --help` 로 fish/powershell 등 다른 셸 방법을 확인하세요.

## AI 에이전트 스킬

`blo` 를 Claude Code 등 AI 에이전트가 활용하도록 [Agent Skill](./skills/blo/SKILL.md)을 제공합니다.

```bash
mkdir -p ~/.claude/skills && cp -r skills/blo ~/.claude/skills/
```

자세한 내용은 [skills/README.md](./skills/README.md) 참고.

## 구조

```
main.go              진입점
cmd/                 cobra 명령 (auth, cost, groups, months)
internal/crypto/     encBody AES-128-CBC + 로그인 RSA (사이트 JS와 1:1, 테스트로 검증)
internal/client/     로그인 + 암호화 AJAX 호출, 세션 쿠키 관리
internal/billing/    도메인 레이어 (요약/추이/서비스·계정 분해, 컨텍스트 해석)
internal/auth/       세션 저장소 (~/.config/billing-on)
internal/output/     table / json / csv 렌더링
skills/              AI 에이전트용 스킬
```

## 개발

```bash
go test ./...        # 테스트 (crypto는 실제 캡처 데이터로 검증)
go vet ./...
gofmt -l .           # 포맷 확인
```

릴리스: `vX.Y.Z` 태그를 푸시하면 GitHub Actions + GoReleaser가 멀티플랫폼
바이너리, 체크섬, Homebrew formula를 자동 발행합니다.

## 면책

이 도구는 비공식이며 ㈜이테크시스템/빌링온과 무관합니다. 본인이 정당하게 접근
권한을 가진 계정의 데이터 조회 용도로만 사용하세요.

## 라이선스

[MIT](./LICENSE)
