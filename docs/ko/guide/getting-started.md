# 시작하기

## GitHub Release에서 다운로드

최신 버전은 다음 두 가지 Windows amd64 실행 파일로 배포됩니다.

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): 인스톨러 버전
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): 설치 없이 실행하는 포터블 버전

릴리스 노트와 기타 파일은 [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 페이지에 있습니다.

릴리스 페이지의 `SHA256SUMS`에는 두 파일의 체크섬이 있습니다. 나히다 데스크탑은 64비트 Windows에서만 실행됩니다. 게임바나나 로그인에는 Microsoft Edge WebView2 런타임이 필요합니다. 인스톨러는 이를 함께 설치하며, 포터블 버전은 미리 설치되어 있어야 합니다.

## 직접 빌드하기

빌드에는 다음 도구가 필요합니다.

- Go (`go.mod`에 지정된 버전)
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build`는 Windows 실행 파일을 `bin/nahida-desktop.exe`에 생성합니다.
