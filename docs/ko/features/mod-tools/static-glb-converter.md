# 정적 GLB 변환기 / 모델 뷰어

이 툴은 모드를 정적 GLB로 변환하는 기능을 제공합니다. 변환된 GLB를 블렌더 또는 온라인 GLB 뷰어에서 열어 모델을 확인해 볼 수 있습니다. 모델 뷰어는 정적 GLB 변환기에 기반해 작동합니다.

::: info
정적 GLB 변환기는 현재 `원신`, `붕괴: 스타레일`, `젠레스 존 제로`, `명조`를 지원하며, 모델 뷰어는 `명일방주: 엔드필드`도 지원합니다.
또한 일부 모드와 대부분의 애니메이션 모드는 정상적으로 변환되지 않을 수 있습니다.
:::

## 옵션

툴 화면에서 다음 옵션을 설정할 수 있습니다.

- **에셋 레이아웃 경로**:
  3dmigoto 에셋 경로를 지정합니다. 이 옵션은 변경시 영구 저장됩니다. 자세한 내용은 [에셋](#에셋)에서 확인하세요.

- **대상 모드 경로**:
  변환될 대상 모드의 경로를 지정합니다.

- **출력 GLB 경로**:
  변환된 glb 모델 파일이 저장될 위치를 선택합니다.

- **텍스처 형식**:
  텍스처 이미지의 포맷을 선택합니다.  
  변환 속도와 glb 크기 절약을 위해 `JPEG (알파 안전)` 모드를 사용하세요.

## 변환

변환을 진행하면 출력 경로에 다음과 같은 폴더와 파일들이 생성됩니다.

```text
출력 경로/
├─ glb/
│  └─ 변환된 GLB 파일
├─ ui/
│  └─ 토글 뷰어 에셋 파일
└─ manifest.json
```

변환된 GLB 파일은 `glb` 폴더 내에서 확인할 수 있습니다.

## 에셋

`원신`, `붕괴: 스타레일`, `젠레스 존 제로`, `명조`의 GLB 생성을 위해서는 각 게임에 대응하는 에셋이 필요합니다.

각 게임의 공식 에셋 저장소는 다음과 같습니다.

- 원신: [SilentNightSound/GI-Model-Importer-Assets](https://github.com/SilentNightSound/GI-Model-Importer-Assets)
- 붕괴: 스타레일: [SilentNightSound/SR-Model-Importer-Assets](https://github.com/SilentNightSound/SR-Model-Importer-Assets)
- 젠레스 존 제로: [leotorrez/ZZ-Model-Importer-Assets](https://github.com/leotorrez/ZZ-Model-Importer-Assets)
- 명조: [SpectrumQT/WWMI-Assets](https://github.com/SpectrumQT/WWMI-Assets)

필요한 게임의 에셋을 다운로드한 뒤, 다음과 같은 구조로 배치합니다.

```text
assets/
├─ GI-Model-Importer-Assets/
├─ SR-Model-Importer-Assets/
├─ ZZ-Model-Importer-Assets/
└─ WWMI-Assets/
```

이후 `assets` 폴더를 **에셋 레이아웃 경로**로 지정합니다.

::: tip
각 저장소가 항상 게임의 최신 상태를 반영하는 것은 아닙니다. 특정 캐릭터 또는 무기의 에셋이 누락되었거나 데이터가 오래된 경우, [gui_collect](https://github.com/Petrascyll/gui_collect) 등의 도구를 사용해 에셋을 직접 덤프할 수 있습니다.
:::

::: tip
GLB 변환에 필요한 에셋 파일은 `vb`, `ib`, `fmt`, `json`, `txt` 파일입니다. 에셋을 GLB 변환 용도로만 사용할 경우, 용량을 많이 차지하는 `dds` 파일은 삭제해도 됩니다.
:::
