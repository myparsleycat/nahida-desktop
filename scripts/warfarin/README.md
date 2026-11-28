# Warfarin(EFMI) 캐릭터 에셋 갱신 — 실행 체크리스트

순서대로 실행하는 체크리스트다. 각 단계의 `기대`와 실제 결과가 다르면 `실패 시`에 적힌 동작만 한다.
실행 위치는 저장소 루트 `E:\Dev\projects\nhd\desktop` 이다.

`scripts/skowt/`(skowt)와 같은 구조이며, 다른 점은 **모든 fetch가 사이트 페이지 안에서 일어난다**는 것이다.
`assets.warfarin.wiki`는 페이지 밖에서 403을 돌려준다.

---

## 0. 현재 상태

- EFMI 카탈로그 36종 = 라이브 36종 (`chr_*`), 모션·original 인덱스도 36행
- 2026-11-28 기준 baseline(`state/captured.json`) 기록 완료
- 재구성 검증: `arclight`를 이 도구로 다시 받아 변환한 결과가 기존 산출물과 바이트 단위로 일치
  (motion `fbd05e6cd37d`, original `ee3e776d0c9e`)
- EFMI는 toon 병합 대상이 아니다 (뷰어가 자체 셰이딩을 쓴다)

---

## 1. 소스 구조

```text
https://assets.warfarin.wiki/studio/<version>/data/catalog-v8.json
  models[<chr_key>] = { name, model: { glb: "<hash>.bin" }, clips: "<hash>.json", ... }
  → 캐릭터 목록은 models 중 키가 chr_ 로 시작하는 항목 (36개)

<version>/data/<hash>.json        클립 목록 [{ file, name, source, duration, samples, loop, bytes }]
<version>/models/<hash>.bin       모델 GLB (sha256 앞 16자가 파일 이름)
<version>/clips/<hash>.bin        클립 GLB (animations 1개)
<version>/textures/<hash>.bin     모델이 참조하는 외부 이미지 버퍼
```

모델 GLB의 `buffers[1..]`는 `../textures/<hash>`를 가리키고, 실제 파일은 `<hash>.bin` 이다.

---

## 2. 사전 확인

```powershell
bun --version
node --version
npx --no-install wrangler whoami
Get-NetTCPConnection -LocalPort 8789 -State Listen -ErrorAction SilentlyContinue
```

기대: 버전 출력, `whoami`가 `nhd-public` 접근 가능 계정, 8789 포트 점유 없음.
실패 시: 포트를 점유한 프로세스를 종료하거나 `npx wrangler login` 후 재확인. 해결되지 않으면 중단.

---

## 3. 수신 서버

별도 터미널에서 유지한다.

```powershell
bun scripts/warfarin/server.mjs
```

기대: `warfarin receiver: http://127.0.0.1:8789/`

---

## 4. 페이지 헬퍼 설치

1. 내장 브라우저에서 `https://studio.warfarin.wiki/` 를 연다.
2. `browser.evaluate` 로 실행한다.

```js
await eval(await (await fetch('http://127.0.0.1:8789/script')).text())
```

기대: `{ version: "https://assets.warfarin.wiki/studio/<32hex>/", characters: 36, targets: <n>, skipped: 0, first: "<slug>" }`

이 단계에서 카탈로그가 `state/live.json` 으로 저장된다 (`POST /catalog`).
`eval` 이 막히면 `scripts/warfarin/capture-page.js` 전체를 붙여넣는다.
새로고침하면 헬퍼가 사라지므로 다시 실행한다.

---

## 5. 대상 산출

4단계로 `state/live.json` 이 생긴 뒤 실행한다.

```powershell
bun scripts/warfarin/diff.mjs
```

기대:

```text
changed <n>, added <m>, skipped <k>, missing <d>
targets: ...
```

- `changed`: baseline의 모델 파일 이름(내용 해시)과 라이브가 다른 캐릭터
- `added`: baseline에 없는 `chr_*`
- `skipped`: `state/skipped.json` 에 적힌 제외 목록
- `missing`: 스튜디오에서 사라진 baseline 항목 (정보성)

`changed`와 `added`가 모두 0이면 받을 것이 없다. 여기서 멈춘다.

`needs an efmi catalog row ...` 가 출력되면 그 캐릭터는
`internal/mod/character/data/efmi.json` 에 항목이 없어 앱에 나오지 않는다.
먼저 카탈로그를 갱신한 뒤 진행한다.

---

## 6. 접근 확인 (유일한 판정 지점)

```js
await window.__war.loadTargets()   // 5단계 결과를 다시 읽는다
await window.__war.testOne()
```

기대: `{ key: "chr_...", slug: "...", bytes: <양수>, ms: <양수> }` 이고 예외 없음.

실패 시: 오류 문자열을 그대로 기록하고 **즉시 중단·보고**한다. 7단계 이후를 실행하지 않는다.

---

## 7. 캡처

```js
await window.__war.runBatch(5)
window.__war.report()
```

- 캐릭터 1개당 약 10–15초(모델 4MB + 클립 15개 + 텍스처 28개), 2초 간격
- `report().idx === report().total` 이 될 때까지 반복한다
- 네트워크 오류는 `await window.__war.retryErrors()` 로 1회만 재시도한다
- 이미 받은 파일은 `work/incoming` 을 보고 자동으로 건너뛴다

기대: `{ idx: <total>, total: <total>, done: <total>, errors: [], running: false }`

---

## 8. 분리

```powershell
bun scripts/warfarin/split.mjs
```

기대: 입력 파일마다 `efmi-<slug>.bin.gz motion,original`.
`work/batches/{motion,original}/efmi-<slug>.bin.gz` 와 `work/raw-original/efmi/<slug>/...` 가 생긴다.

---

## 9. 변환

```powershell
node scripts/import-character-motion.mjs scripts/warfarin/work/batches/motion scripts/warfarin/work/out-motion
node scripts/import-character-original.mjs scripts/warfarin/work/batches/original scripts/warfarin/work/out-original
```

기대:
- motion: `efmi/<slug>: N clips, ... -> ... MB (rest ..., unmatched 0, shape keys 0)`
- original: `efmi/<slug>: N vertices, ... ; T textures ...`

`unmatched`가 0이 아니면 중단·보고.

---

## 10. 인덱스 병합 + 업로드

skowt용 공용 스크립트에 작업 폴더를 넘겨 쓴다.

```powershell
bun scripts/skowt/merge-index.mjs scripts/warfarin/work
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group motion -Work scripts/warfarin/work/upload -Throttle 8
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group original -Work scripts/warfarin/work/upload -Throttle 10
```

기대: `motion: N files, <rows> characters` / `original: N files, ...` / `textures: T`,
이어서 `uploaded X/X files, ... MB` 두 번.

업로드 버킷은 skowt와 같다 (`nhd-public/desktop/character-motion`, `.../character-original`).

대체된 객체 정리(기존 캐릭터를 다시 받았을 때만):

```powershell
bun scripts/skowt/orphans.mjs scripts/warfarin/work/upload
pwsh -NoProfile -File scripts/skowt/delete-orphans.ps1 -Manifest scripts/warfarin/work/r2-orphans.json
```

---

## 11. 검증

```powershell
npx --no-install wrangler r2 object get nhd-public/desktop/character-motion/index.json --file scripts/warfarin/work/verify-motion.json --remote
(Get-FileHash scripts/warfarin/work/verify-motion.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_motion_index.json).Hash
npx --no-install wrangler r2 object get nhd-public/desktop/character-original/index.json --file scripts/warfarin/work/verify-original.json --remote
(Get-FileHash scripts/warfarin/work/verify-original.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_original_index.json).Hash
```

기대: 둘 다 `True`. 모델·텍스처 샘플 검증은 `scripts/skowt/README.md` 13장과 같은 방식이다.

---

## 12. baseline 갱신

```powershell
bun scripts/warfarin/mark-captured.mjs
```

기대: `baseline rows <n>, added <a>, updated <u>`.
`--all` 을 붙이면 라이브 전체로 baseline을 다시 만든다 (최초 1회용, 2026-11-28에 수행됨).

---

## 13. 보고

```text
- 캡처: <n>/<target>
- 변환: motion <n>, original <n>, textures <n>
- 업로드: motion <n> files <mb>, original <n> files <mb>
- R2 검증: index <true/false>
- build/test/lint: <pass/fail>
- 실패 시: 대상 slug, 요청 URL, status, 오류 메시지
```

`task build` / `task test` / `task lint` 는 한 번에 하나씩, 2분 제한으로 실행한다.

---

## 14. 데이터 형식

### 14.1 업로드 레코드

`split.mjs` 는 gzip record stream(`[u32 nameLen][u32 dataLen][name][data]`)을 그대로 나눈다.

```text
motion/efmi/<slug>/model.glb        lite 모델 (아래 14.3)
motion/efmi/<slug>/clips/<name>.glb 클립 GLB
motion/efmi/<slug>/meta.json        { source, key, name, clips[], materials[] }
original/efmi/<slug>/model.glb      full 모델 GLB
original/efmi/<slug>/textures/<hash> 외부 이미지 버퍼 (.bin 확장자 없이)
original/efmi/<slug>/meta.json      { source, key, name }
```

`meta.clips[]` 는 `{ name, source, duration, samples, loop }` 이고 `name` 은
`import-character-motion.mjs` 의 `EFMI_CLIPS` 키와 같아야 한다.

### 14.2 클립 선택과 순서

화이트리스트는 `EFMI_CLIPS` 키를 그대로 쓴다.

```text
idle_loop run_loop sprint_loop walk_loop battle_loop battle_normal_skill battle_skill_ult
gacha_ani gacha_ani_loop interact_sit_loop relax_loop relax_sp_01 relax_sp_02
ui_overview_loop ui_relax_loop uiteam_idle_01
```

사이트 클립 이름은 `A_actor_<actor>_<name>` 형태이므로 앞부분을 잘라 이름을 얻는다.
`capture-page.js` 의 나열 순서는 2026-11-28 배치와 같게 맞춰 두었다. 순서가 바뀌면 같은 캐릭터를
다시 받았을 때 결과 파일 해시가 달라진다.

### 14.3 lite 모델

`import-character-motion.mjs` 는 메시의 `primitives[].extras.material` 과 `extras.indices` 를 읽는다.
따라서 모션 쪽 model.glb 는 full 모델이 아니라 다음만 남긴 lite 모델이어야 한다.

- 노드는 `name, translation, rotation, scale, matrix, children` 만
- 스킨드 노드(`mesh != null && skin != null`)의 메시만, 속성은 `POSITION, JOINTS_0, WEIGHTS_0, JOINTS_1, WEIGHTS_1`
- 각 primitive `extras = { material: <재질 이름>, indices: <인덱스 수> }`
- 스킨은 `joints` + `inverseBindMatrices` (+ `skeleton`)
- 위에 쓰인 accessor/bufferView만 복사

full 모델을 그대로 보내면 재질·인덱스 정보가 없어 변환 결과가 달라진다.

---

## 15. 파일 구성

```text
state/
  live.json       스튜디오 카탈로그 스냅샷 (4단계, gitignore)
  captured.json   캡처 baseline: chr_key -> { name, model }  (커밋 대상)
  diff.json       changed / added / missing / targets (gitignore)
  targets.json    이번 실행 대상 (gitignore)
  remaining.json  수신 서버가 /targets 로 제공 (gitignore)
scripts/
  server.mjs          127.0.0.1:8789 수신 서버
  capture-page.js     페이지 캡처 헬퍼 (카탈로그 전송 포함)
  split.mjs           record stream 분리
  diff.mjs            baseline 비교 + 대상 산출
  mark-captured.mjs   baseline 갱신
  README.md
```

변환·병합·업로드는 공용 스크립트를 쓴다: `scripts/import-character-{motion,original}.mjs`,
`scripts/skowt/merge-index.mjs`, `scripts/skowt/upload.ps1`, `scripts/skowt/orphans.mjs`.
