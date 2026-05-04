# 시작하기

## GitHub Release에서 다운로드

[GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 페이지에서 `Nahida-Desktop-Setup-x.x.x.exe` 파일을 클릭해 최신 버전의 인스톨러를 다운로드할 수 있습니다.

## 직접 빌드하기

빌드하기 전에 다음 도구가 설치되어 있어야 합니다.

- pnpm v10
- Rust toolchain (`cargo`, `rustc` 포함)

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
pnpm install && pnpm run build:native
pnpm run build:win
```
