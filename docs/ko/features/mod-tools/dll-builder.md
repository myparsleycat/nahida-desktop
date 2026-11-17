# 4001 Fixer

원신의 `4001` 오류(불법 프로그램 감지)는 XXMI에 포함된 기본 DLL 대신 `XXMI-Libs-Package` 소스에서 빌드한 DLL을 사용하면 해결되는 경우가 많습니다.

`4001 Fixer`는 교체용 `d3d11.dll`을 빌드하거나 기존 DLL에 PE 패딩 다양화를 적용합니다.

## 필수 빌드 도구 설치

빌드 도구는 직접 설치해야 합니다. Git은 별도로 설치할 필요가 없습니다. 나히다 데스크탑이 첫 빌드 때 포터블 Git(MinGit, 약 40MB)을 다운로드하여 앱 데이터 폴더에서 사용합니다.

1. [Visual Studio Build Tools 2022](https://aka.ms/vs/17/release/vs_BuildTools.exe)를 다운로드한 뒤 실행합니다.
2. 개별 구성 요소에서 다음 항목을 선택한 후 설치합니다.
   - **MSVC v143 - VS 2022 C++ x64/x86 빌드 도구(최신)**
   - **Windows 11 SDK(10.0.22621.0)**

::: warning
빌드 도구 설치에는 약 4.4GB의 디스크 공간이 필요합니다.
:::

## XXMI 연결하기

이 기능을 사용하려면 먼저 XXMI를 설정해야 합니다. 자세한 내용은 [XXMI 연결하기](/ko/others/set-up-xxmi)를 참고합니다.

## DLL 빌드

빌드 화면에는 다음 옵션이 있습니다.

- **제공자**: 빌드할 `XXMI-Libs-Package` 저장소를 선택합니다. `myparsleycat`은 `XXMI-Libs-Package-Forked` 포크를 빌드합니다.
- **버전**: 빌드할 XXMI 라이브러리 버전을 선택합니다.
- **대상 GIMI 임포터**: 빌드한 DLL을 적용할 GIMI Importer를 선택합니다. `4001` 오류는 원신에서만 발생하므로 GIMI Importer만 표시됩니다.

**빌드 시작** 버튼을 클릭하면 빌드가 시작됩니다. Windows SDK 헤더(`crtdbg.h` 등)나 필요한 SDK 버전을 찾지 못해 빌드가 실패하면 설치 또는 복구 안내가 표시됩니다. SDK가 이미 설치되어 있다면 Visual Studio Installer에서 해당 설치를 복구하거나, **Visual Studio 경로**에 Visual Studio 또는 Build Tools 2022의 설치 폴더나 `vcvars64.bat`를 지정합니다.

## DLL 패딩 다양화

**DLL 패딩 다양화** 탭은 선택한 GIMI Importer의 `d3d11.dll`에서 레이아웃을 바꾸지 않고 PE 패딩 바이트를 다양화합니다. 적용 전에 같은 폴더에 `pepd` 백업 파일을 생성하며, 다양화된 DLL이 적용되어 있는 동안 복원 동작이 표시됩니다.

## 시스템 폴더와 관리자 권한

GIMI Importer가 `C:\Program Files\` 같은 보호된 위치에 있으면 DLL을 설치할 때 Windows UAC 프롬프트가 표시됩니다. 프롬프트를 거부하면 작업이 실패합니다. 게임이 `d3d11.dll`을 사용 중이면 파일을 교체할 수 없으므로 먼저 게임을 종료해야 합니다.

## 안전하지 않은 모드

XXMI는 **안전하지 않은 모드**가 꺼져 있으면 공식 제공 DLL이 아닌 DLL에 대해 경고를 표시합니다. 4001 Fixer로 DLL을 적용하면 GIMI Importer의 안전하지 않은 모드가 자동으로 켜집니다. 그래도 `d3d11.dll` 서명이 유효하지 않다는 경고가 표시되면 직접 켭니다.

- **내장 런처**: XXMI 페이지에서 GIMI Importer를 열고 `고급` 탭에서 `안전하지 않은 모드`를 켭니다.
- **XXMI 런처**: XXMI 런처 설정에서 안전하지 않은 모드를 켭니다.

이후 게임을 다시 실행합니다. 내장 런처에서 서명된 DLL로 되돌리려면 Importer의 `패키지` 탭에서 `공식 DLL로 복원`을 클릭합니다.
