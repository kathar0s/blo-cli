# blo skills

AI 에이전트(Claude Code 등)가 `blo` CLI를 활용하도록 돕는 [Agent Skills](https://docs.claude.com/en/docs/agents/skills).

## 설치

스킬을 에이전트가 인식하는 위치에 복사(또는 심볼릭 링크)한다.

```bash
# Claude Code 전역 스킬로 설치
mkdir -p ~/.claude/skills
cp -r skills/blo ~/.claude/skills/

# 또는 레포를 클론해 심볼릭 링크 (업데이트 자동 반영)
ln -s "$(pwd)/skills/blo" ~/.claude/skills/blo
```

설치 후 에이전트에게 AWS 비용을 물으면 `blo` 스킬이 자동으로 로드된다.

## 스킬 목록

| 스킬 | 설명 |
|---|---|
| [`blo`](./blo/SKILL.md) | 빌링온 AWS 비용 조회·분석 (`blo` CLI 사용 규약) |
