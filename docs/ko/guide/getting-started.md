# 시작하기

## GitHub Release에서 다운로드

최신 버전은 다음 두 가지 Windows amd64 실행 파일로 배포됩니다.

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): 설치해서 사용하는 인스톨러 버전
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): 설치 없이 실행하는 포터블 버전

변경 사항과 다른 릴리스 자산은 [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 페이지에서 확인할 수 있습니다.

## 직접 빌드하기

빌드하기 전에 다음 도구가 설치되어 있어야 합니다.

- Go (`go.mod`에 지정된 버전)
- Node.js 22
- pnpm v11
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build`는 프런트엔드 의존성 설치와 빌드, Wails 바인딩 생성을 수행한 뒤 Windows 실행 파일을 `bin/nahida-desktop.exe`에 생성합니다. Wails v3 CLI는 `go.mod`에 고정된 버전을 `go tool wails3`로 실행하므로 별도로 설치할 필요가 없습니다.
