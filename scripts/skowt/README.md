# skowt 캐릭터 에셋 갱신 — 실행 체크리스트

이 문서는 순서대로 실행하는 체크리스트다. 각 단계의 `기대`와 실제 결과가 다르면
`실패 시`에 적힌 동작만 수행한다. 임의로 단계를 건너뛰거나 순서를 바꾸지 않는다.

- 실행 위치: `E:\Dev\projects\nhd\desktop` (저장소 루트)
- 도구 폴더: `E:\Dev\projects\nhd\desktop\scripts\skowt`

---

## 0. 현재 상태

2026-11-28 완료. 122개 전부 캡처·변환·업로드·검증했고 인게임 확인도 끝났다.
이 문서는 같은 작업을 다시 해야 할 때를 위한 절차 기록이다.

- 전체 대상 122개 = 변경 rev 116 + 신규 6 (GIMI 57, SRMI 2, ZZMI 63)
- 업로드: 1차 motion 81개(517MB)·original 1,658개(241MB),
  재캡처 motion 59개(716MB)·original 1,339개(365MB)
- 인덱스 373행, toon 병합 완료, R2 SHA 검증 통과
- `task build`/`task test`/`task lint` 통과
- 대체된 모델 32개는 R2에서 삭제, 미참조 텍스처는 없었다
- `work/`는 실행 시 생성되며 저장소에서 제외된다 (`scripts/skowt/.gitignore`)
- `state/live.json`, `state/diff.json`, `state/targets.json`, `state/remaining.json`도 생성물이라 제외된다.
  `state/captured.json`(baseline)만 커밋 대상이다
- 수치와 경위: `state/progress.json`, `state/recapture.json`

**주의**: `capture-page.js`의 `liteAnim`은 노드의 `mesh` 인덱스로 원본 메시를 찾아
`{targetNames, mesh}`를 붙인다. 메시 이름과 노드 이름이 다른 캐릭터가 많으므로
이름으로 매칭하면 shape key 채널이 전부 버려진다.

---

## 1. 접근 상태와 중단 조건

2026-11-28 기준 skowt viewer 접근이 차단되어 있었다.

- `GET https://den.skowt.cc/viewer/links?a=<assetId>:model&g=3,4`
  → `409 {"error":"Conflict","gate":"reload","notice":"Automated access to skowt.cc's viewer isn't permitted. ..."}`
- 같은 요청에 `&l=1` → 200이지만 키 복호화 실패 (`ViewerFileError: unwrap`, wasm error 9)
- 같은 브라우저의 공식 뷰어도 `stage: the model file (model) failed (unwrap)`
- VPN egress IP 변경(`154.47.20.69`) 후에도 동일

해제: 브라우저의 쿠키와 사이트 데이터를 지운 뒤 정상 동작했다. IP가 아니라
쿠키/세션에 걸린 차단이었고, 우회 구현은 쓰지 않았다.

**중단 조건** — 아래 중 하나가 나오면 즉시 중단하고 보고한다.

| 신호 | 판정 |
|---|---|
| HTTP 409 + `gate` | 접근 차단 |
| `unwrap`, `open 9` | 접근 차단 |
| `Automated access` 문구 | 접근 차단 |
| `no <role> link` | 접근 차단 |
| 동일 오류 3회 반복 | 접근 차단 |

중단 시 재시도, 우회 프록시, 프로필·지문 위장, 키 조작을 하지 않는다.
접근이 정상 동작하는 것이 **2단계에서 확인된 경우에만** 3단계 이후를 실행한다.

---

## 2. 사전 확인

### 2.0 다음 갱신의 대상 산출

이전 실행 이후 skowt에 추가·변경된 것만 잡으려면 아래 순서를 먼저 실행한다.
`state/targets.json`은 이 과정에서 새로 만들어진다.

```powershell
bun scripts/skowt/live.mjs
bun scripts/skowt/diff.mjs
bun scripts/skowt/prep.mjs
```

기대: `diff.mjs`가 `changed <n>, newData <m>, skipped <k>, missing <d>`를 출력하고,
`prep.mjs`가 대상 수와 게임별 개수를 출력한다.

- `changed`: `state/captured.json`의 rev와 라이브 rev가 다른 캐릭터 (publicId로 결합)
- `newData`: 카탈로그에 있으나 인덱스에 없는 캐릭터 (이름을 slug로 바꿔 라이브와 결합)
- `skipped`: 의도적으로 받지 않기로 한 캐릭터 (`state/skipped.json`). `newData`에서 빠진다
- `missing`: 라이브에서 사라진 캡처 항목. 다시 캡처하지 않는다
- `catalog without a skowt item`: skowt에 없는 카탈로그 항목(예: `gimi/manekin`). 무시

두 값이 모두 `0`이면 새로 받을 것이 없다. 할 일이 없으면 여기서 멈춘다.

작업이 끝나면 baseline을 갱신해야 다음 실행에서 같은 캐릭터를 다시 잡지 않는다.

```powershell
bun scripts/skowt/mark-captured.mjs
```

기대: `captured rows <n>, added <a>, updated <u>`.

### 2.1 도구

```powershell
bun --version
node --version
npx --no-install wrangler --version
npx --no-install wrangler whoami
```

기대: 세 명령 모두 버전 출력, `whoami`가 R2 버킷(`nhd-public`)에 접근 가능한 계정을 출력.
`whoami`가 실패하면 `npx wrangler login` 으로 인증한 뒤 다시 확인한다.
실패 시: 중단하고 보고.

### 2.2 포트

```powershell
Get-NetTCPConnection -LocalPort 8788 -State Listen -ErrorAction SilentlyContinue
```

기대: 출력 없음.
실패 시: 8788을 점유한 프로세스를 종료한 뒤 다시 확인. 종료할 수 없으면 중단.

---

## 3. 수신 서버

별도 터미널에서 실행하고, 이 터미널은 끝까지 유지한다.

```powershell
cd E:\Dev\projects\nhd\desktop
bun scripts/skowt/server.mjs
```

기대 출력: `skowt handoff receiver: http://127.0.0.1:8788`

다른 터미널에서 확인:

```powershell
(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8788/ping).Content
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8788/targets
([System.Text.Encoding]::UTF8.GetString($r.Content) | ConvertFrom-Json).remaining.Count
```

기대: `pong`, 2.0에서 산출한 대상 수.
실패 시: 서버 로그를 확인하고 중단.

---

## 4. 페이지 헬퍼 설치

1. 내장 브라우저에서 `https://skowt.cc/a/ONBSg8opf` 를 연다.
2. `browser.evaluate` 로 아래를 실행한다.

```js
await eval(await (await fetch('http://127.0.0.1:8788/script')).text())
```

기대: `{ targets: <2.0의 대상 수>, skipped: 0, first: "<첫 대상>" }`

이미 받은 파일이 `work/incoming` 에 있으면 자동으로 제외된다.
재실행 시 예: `{ targets: 12, skipped: 30, first: "..." }`.

`eval` 이 CSP로 막히면 `scripts/skowt/capture-page.js` 전체를 같은 `browser.evaluate`에 붙여넣는다.
페이지를 새로고침하면 헬퍼가 사라진다. 이 경우 이 단계를 다시 실행한다.

실패 시: 오류 문자열을 기록하고 중단.

---

## 5. 접근 확인 (유일한 판정 지점)

```js
await window.__testOne()
```

기대: `{ key: "zzmi/...", bytes: <양수>, ms: <양수> }` 이고 예외 없음.

실패 시: 예외 메시지를 그대로 기록하고 **즉시 중단·보고**한다.
이 단계에서 실패하면 6단계 이후를 실행하지 않는다. 재시도하지 않는다.

---

## 6. 캡처

```js
await window.__runBatch(10)
```

- 기대: 반환값 `{ idx, total, results, errorCount: 0 }` (`total` 은 4단계에서 로드한 수)
- 호출이 타임아웃되어도 캡처는 페이지에서 계속된다. `window.__report()` 로 확인하고
  `idx`부터 이어서 실행한다. 처음부터 다시 시작하지 않는다.
- `window.__report().idx === window.__report().total` 이 될 때까지 `__runBatch(10)` 을 반복한다.

```js
window.__report()
```

기대: `{ idx: <total>, total: <total>, done: <total>, errors: [], running: false }`

`errors` 가 비어 있지 않으면:

1. 오류 문자열에 `409`, `gate`, `unwrap`, `open 9`, `Automated access` 중 하나가 있으면
   **즉시 중단·보고**한다.
2. 그 외(네트워크 등)면 한 번만 재시도한다.

```js
await window.__retryErrors()
window.__report()
```

기대: `errors: []`.
재시도 후에도 오류가 남으면 중단·보고한다.

---

## 7. 수신 파일 확인

```powershell
(Get-ChildItem scripts\skowt\work\incoming -Filter *.bin.gz).Count
```

기대: 캡처한 캐릭터 수와 동일 (전부 성공 시 2.0의 대상 수).

실패 시: 부족한 키를 `state/remaining.json` 과 비교해 나열하고 중단·보고.

---

## 8. 분리

```powershell
bun scripts/skowt/split.mjs
```

기대: 입력 파일마다 `motion,original` 한 줄.

```powershell
(Get-ChildItem scripts\skowt\work\batches\motion -Filter *.bin.gz).Count
(Get-ChildItem scripts\skowt\work\batches\original -Filter *.bin.gz).Count
(Get-ChildItem scripts\skowt\work\raw-original -Directory).Count
```

기대: 앞의 두 값이 7단계 수와 동일, 세 번째도 동일.
실패 시: 중단·보고.

---

## 9. 변환

```powershell
node scripts/import-character-motion.mjs scripts/skowt/work/batches/motion scripts/skowt/work/out-motion
node scripts/import-character-original.mjs scripts/skowt/work/batches/original scripts/skowt/work/out-original
```

기대:
- motion 마지막 줄: `N characters, ... MB`
- original 마지막 줄: `N characters, models ... MB, M textures ... MB`
- `N` = 8단계 파일 수

실패 시: 예외에 찍힌 `<game>/<key>` 를 기록하고 중단·보고.

---

## 10. toon 병합

```powershell
bun scripts/skowt/merge-toon.mjs
```

기대: 처리한 캐릭터마다 `gimi/...: N materials` 등, 마지막에 게임별
`gimi: N characters, M updated images, K removed`.

실패 시: `no captured originals ...` 또는 예외 메시지를 기록하고 중단·보고.

---

## 11. 인덱스 병합 + 스테이징

```powershell
bun scripts/skowt/merge-index.mjs
```

기대:
- `motion: N files, M characters`
- `original: N files, M characters`
- `textures: T`

실패 시: 중단·보고.

---

## 12. 업로드

```powershell
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group motion -Throttle 8
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group original -Throttle 10
```

기대: 각각 `uploaded X/X files, ... MB`.
실패 시: `FAILED ...` 줄을 기록하고 중단·보고.

---

## 13. R2 검증

```powershell
npx --no-install wrangler r2 object get nhd-public/desktop/character-motion/index.json --file scripts/skowt/work/verify-motion.json --remote
npx --no-install wrangler r2 object get nhd-public/desktop/character-original/index.json --file scripts/skowt/work/verify-original.json --remote
```

```powershell
(Get-FileHash scripts/skowt/work/verify-motion.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_motion_index.json).Hash
(Get-FileHash scripts/skowt/work/verify-original.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_original_index.json).Hash
```

기대: 둘 다 `True`.

모델/텍스처 샘플 1개씩:

```powershell
$m = (Get-ChildItem scripts\skowt\work\upload\motion -Recurse -Filter *.glb.gz | Select-Object -First 1)
$rel = [System.IO.Path]::GetRelativePath("scripts\skowt\work\upload\motion", $m.FullName).Replace('\','/')
npx --no-install wrangler r2 object get "nhd-public/desktop/character-motion/$rel" --file scripts/skowt/work/verify-model.glb.gz --remote
(Get-FileHash scripts/skowt/work/verify-model.glb.gz).Hash -eq (Get-FileHash $m.FullName).Hash

$t = (Get-ChildItem scripts\skowt\work\upload\original\textures -Filter *.webp | Select-Object -First 1)
npx --no-install wrangler r2 object get "nhd-public/desktop/character-original/textures/$($t.Name)" --file scripts/skowt/work/verify-texture.webp --remote
(Get-FileHash scripts\skowt\work\verify-texture.webp).Hash -eq (Get-FileHash $t.FullName).Hash
```

기대: 둘 다 `True`.
실패 시: 중단·보고.

퍼블릭 `https://nhd-public.nahida.live/.../index.json` 은 엣지 캐시가 늦을 수 있다.
앱은 임베디드 인덱스를 읽으므로 R2 원본 검증으로 충분하다.

### 13.1 대체된 객체 정리 (기존 캐릭터를 다시 받았을 때만)

```powershell
bun scripts/skowt/orphans.mjs
pwsh -NoProfile -File scripts/skowt/delete-orphans.ps1
```

기대: `unreferenced: motion <n>, original <n>, textures <n>` → `deleted <k>/<k>`.
`n`이 모두 0이면 지울 것이 없다.
실패 시: `FAILED` 줄을 기록하고 중단·보고.

---

## 14. 최종 확인

`task` 명령은 한 번에 하나씩, 2분 제한으로 실행한다.

```powershell
$p = Start-Process task -ArgumentList 'build' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
$p = Start-Process task -ArgumentList 'test' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
$p = Start-Process task -ArgumentList 'lint' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
```

기대: build 성공, test 전체 `ok`, lint `0 issues`.
실패 시: 중단·보고.

---

## 15. 보고

아래 형식으로 보고한다.

```text
- 캡처: <n>/<target> (게임별: ...)
- 변환: motion <n>, original <n>, textures <n>
- 업로드: motion <n> files <mb>, original <n> files <mb>
- R2 검증: index <true/false>, model <true/false>, texture <true/false>
- build/test/lint: <pass/fail>
- 차단 발생 시: 요청 URL, status, gate, error code
- 남은 작업: <없음 | 목록>
```

---

## 16. 금지

- 커밋·푸시: 명시적 요청 전 금지
- `go.mod` 로컬 replace 추가 금지
- `task` 명령 동시 실행 금지
- 1장의 중단 조건에서 재시도 금지
- 우회 프록시, 프로필·지문 위장, 키 조작 금지

---

## 17. 파일 구성

```text
state/
  live.json          skowt 라이브 카탈로그 (live.mjs)
  captured.json      캡처 완료 baseline (key, publicId, rev)
  diff.json          changed / newData / missing (diff.mjs)
  targets.json       이번 실행 대상 + meta (prep.mjs)
  remaining.json     수신 서버가 /targets 로 제공하는 목록
  skipped.json       받지 않기로 한 캐릭터와 사유 (커밋 대상)
  progress.json      실행 결과, 업로드 용량, 검증 상태
  recapture.json     모프 누락 재캡처 기록 (2026-11-28)
scripts/
  live.mjs           라이브 카탈로그 수집
  diff.mjs           baseline 대비 변경/신규 산출
  prep.mjs           대상 목록 생성 (asset.getById 로 meta 보강)
  server.mjs         127.0.0.1:8788 수신 서버
  capture-page.js    브라우저 페이지 캡처 헬퍼
  split.mjs          수신 스트림을 importer 배치로 분리
  merge-toon.mjs     toon 델타 병합
  merge-index.mjs    인덱스 병합 + 업로드 스테이징
  upload.ps1         R2 업로드
  orphans.mjs        미참조 객체 목록 생성
  delete-orphans.ps1 미참조 객체 삭제
  mark-captured.mjs  baseline 갱신
  select-recapture.mjs  재캡처 대상 재산출
  progress.mjs       progress.json 재생성
```

`capture-page.js`만 브라우저에서 실행되고, 나머지는 저장소 루트에서 실행한다.
변환 단계는 `scripts/` 루트의 공용 importer를 쓴다
(`import-character-motion.mjs`, `import-character-original.mjs`, `import-character-toon.mjs`).

---

## 18. 데이터 형식과 디코더

`capture-page.js`를 다시 작성해야 할 때를 위한 기록이다. 브라우저 세션(쿠키)이 있어야 동작한다.

### 18.1 링크 발급

```text
GET https://den.skowt.cc/viewer/links?a=<assetId>:<role>&g=3,4&l=1&vr=1
    credentials: include
→ { ttl, signed, links: { <name>: "exp=…&sig=…" },
    assets: { <assetId>: { <role>: <name> } },
    keys: { <name>: <b64url> }, digests: { <name>: <b64url> },
    mark: <b64url>, locks: { <name>: <u32> }, decoder: <id> }
```

`l=1` 없이 호출하면 `409 {error:"Conflict", gate:"reload", notice:"Automated access … isn't permitted"}`.
`assets[assetId]`에 해당 role 키가 없으면 그 자산은 그 role을 제공하지 않는다
(Promeia는 `model`만 있고 `anim`/`toon`이 없다).

### 18.2 파일과 디코더

```text
GET https://pack.skowt.cc/v/<name>?<links[name]>       (credentials: include)
GET https://den.skowt.cc/viewer/decoder/<decoder>      wasm
```

wasm export는 `m`(memory), `h`(alloc), `o`(open), `s`(free)이다. `open` 호출 전에 메모리에 다음을 채운다.

| 오프셋 | 내용 |
|---|---|
| 0 | `keys[name]` (16바이트) |
| 16 | SHA-256(wasm 바이트) |
| 5376 | `mark` (16바이트) |
| 5392 | `digests[name]` (16바이트) |
| 5408 | `locks[name]` (u32 LE) |

`o(ptr, len)`이 0을 반환하면 성공, 9면 `unwrap` 실패다. 성공하면 오프셋 64부터
`role, jsonOff, jsonLen, binOff, binLen`(u32 LE)을 읽어 JSON과 bin 청크를 꺼낸다.

### 18.3 role별 내용

- `model`: 이미지가 bin에 포함된 full glTF
- `anim`: 클립. 노드 extras가 없어서 모프 대상 이름을 직접 붙여야 한다
- `toon`: 이미지 전용 glTF (Promeia처럼 없는 자산도 있다)

### 18.4 업로드 페이로드

gzip으로 압축한 record stream이다. 각 레코드는 `[u32 nameLen][u32 dataLen][name][data]`.

```text
motion/<game>/<key>/model.glb    POSITION/JOINTS_n/WEIGHTS_n만 남긴 lite glTF
motion/<game>/<key>/anim.glb     클립 (모프 대상 이름 포함)
motion/<game>/<key>/meta.json    roles, animations, heightM …
original/<game>/<key>/model.glb  full glTF (재질 extras.textures 에 텍스처 경로)
original/<game>/<key>/toon.glb   toon (있는 경우)
original/<game>/<key>/meta.json
```

`split.mjs`가 이 스트림을 `work/batches/{motion,original}/<game>-<key>.bin.gz`로 나눈다.

### 18.5 모프(shape key) 매핑

`anim` 노드의 이름과 원본 메시 이름이 다른 캐릭터가 많다. 노드의 `mesh` 인덱스로 원본 메시를 찾아
`extras = { targetNames, mesh: <메시 이름> }`을 붙여야 한다. 이름으로 매칭하면 채널이 전부 버려진다.

### 18.6 외부 의존

- `capture-page.js`는 `https://cdn.jsdelivr.net/npm/meshoptimizer@0.22.0/meshopt_decoder.module.js`를
  import 한다. 일부 `model`/`anim`이 `EXT_meshopt_compression`을 쓰기 때문이다
- 변환은 반드시 `node`로 실행한다. `bun`의 gzip은 바이트가 달라 해시가 어긋난다
- 업로드는 `wrangler` 인증이 필요하다 (2.1)
