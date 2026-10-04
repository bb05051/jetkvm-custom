# JetKVM 커스텀 펌웨어

원본 [jetkvm/kvm](https://github.com/jetkvm/kvm) 위에 아래 수정 사항을 커밋으로 얹은 `custom` 브랜치입니다.
새 펌웨어가 나오면 `custom/update.sh`로 수정 사항을 다시 얹고, `custom/deploy.sh`로 장치에 올립니다.

## 구조

```
origin/dev, release/*   ← 원본 (git remote: origin = github.com/jetkvm/kvm)
        │
   custom/base (태그)    ← 수정 사항이 얹혀 있는 원본 커밋
        │
   ├─ chore: custom firmware tooling          (이 폴더. 맨 앞에 둬야 rebase 중에도 스크립트를 쓸 수 있음)
   ├─ feat: touch input on the video and a two-finger USB touchscreen
   ├─ feat(ui): console action bar changes
   ├─ feat: custom EDID presets
   ├─ feat: disable automatic updates in the custom firmware
   ├─ fix(ui): keep touchpad pinch from zooming the browser over the video
   ├─ feat(ui): black dark theme instead of navy
   ├─ feat: allow five simultaneous touches on the USB touchscreen
   ├─ feat: fit the resolution to the browser window
   ├─ feat: aim fitted resolutions at about 1600x900
   ├─ feat(ui): auto fit the resolution when the video area changes
   ├─ feat(ui): show the resolution options in a panel like Paste text
   ├─ feat(ui): separate auto fit for fullscreen
   ├─ fix: keep the TC358743 fed on wide, low pixel clock modes
   ├─ feat: fit base sizes and resolution safe mode
   ├─ feat: space EDID changes 10 s apart and skip fits while taken over
   ├─ docs(custom): document the GitHub fork
   └─ revert: restore edid_presets.go to upstream
        │
   custom (브랜치)
```

`git log --oneline custom/base..custom` 으로 수정 커밋 목록을 볼 수 있습니다.

## GitHub 포크

- 포크: https://github.com/bb05051/jetkvm-custom (git remote 이름 `fork`, 원본은 `origin`)
- 올라가 있는 것: `custom` 브랜치(수정 커밋들)와 `custom/base` 태그
- 평소 변경 후: `git push fork custom`
- `custom/update.sh`로 새 펌웨어 위로 옮긴 뒤에는 기록이 다시 쓰이므로:
  ```bash
  git push --force-with-lease fork custom
  git push --force fork refs/tags/custom/base
  ```
- 다른 PC에서 시작할 때:
  ```bash
  git clone -b custom git@github.com:bb05051/jetkvm-custom.git kvm && cd kvm
  git remote rename origin fork
  git remote add origin https://github.com/jetkvm/kvm.git
  git fetch origin --tags && git fetch fork --tags
  ```

## 새 펌웨어가 나왔을 때

```bash
cd /mainfolder/kvm
custom/update.sh                # 최신 정식 릴리스(release/X.Y.Z) 위로 옮기기
custom/update.sh dev            # 또는 원본 dev 브랜치 최신 위로
custom/update.sh release/0.6.0  # 또는 특정 버전 위로
```

스크립트가 하는 일:
1. 원본 저장소를 받아오고(`git fetch`), 지금 상태를 `custom-backup` 브랜치에 백업합니다.
2. `custom/base..custom` 의 수정 커밋만 새 버전 위로 옮깁니다(`git rebase --onto`).
3. 성공하면 `custom/base` 태그를 새 버전으로 옮기고 검사를 돌립니다.
   UI: `npm ci`(필요할 때) → i18n → `tsc` → `oxlint` / Go: `go vet` → `go test`

### 충돌이 나면

스크립트가 충돌 파일을 보여주고 멈춥니다. 파일을 고친 뒤:

```bash
git add <고친 파일>
custom/update.sh --continue     # 이어서 진행 + 검사
custom/update.sh --abort        # 포기하고 원래대로
```

충돌을 해결하기 어려우면 Claude Code에게 "custom/update.sh 충돌 해결해줘"라고 맡겨도 됩니다.

## 장치에 올리기

```bash
custom/deploy.sh 10.1.1.150      # 테스트 실행 (재부팅하면 원래 펌웨어로 돌아감)
custom/deploy.sh -i 10.1.1.150   # 정식 설치 (jetkvm_app 교체 후 재부팅)
custom/deploy.sh --recover 10.1.1.150        # 테스트 실행을 끝내고 설치된 앱을 다시 실행
custom/deploy.sh --restore-stock 10.1.1.150  # 처음 설치 전에 백업한 원래 앱으로 되돌리기
```

- 정식 설치는 버전을 `0.5.9+custom.<커밋>`처럼 표시합니다(`+` 뒤는 업데이트 비교에서 무시됨).
- 처음 설치할 때 장치의 원래 앱을 `/userdata/jetkvm/bin/jetkvm_app.stock`으로 한 번 백업합니다.
  설치 후 USB가 돌아오지 않으면 이 백업으로 자동 복구합니다.

- 장치 설정에서 **Developer Mode**가 켜져 있고 이 PC의 SSH 키가 등록되어 있어야 합니다.
- 배포 후 90초 안에 USB가 `configured`가 되지 않으면 자동으로 복구합니다
  (개발 앱 종료 → 터치스크린 구성 제거 → USB 컨트롤러 재연결 → 원래 앱 실행).
- 테스트 실행은 스크립트가 끝나지 않고 계속 붙어 있습니다. Ctrl+C로 끝내면 개발 앱도 종료되고,
  장치의 watchdog이 재부팅하면서 원래 펌웨어로 돌아갑니다.
- 커스텀 펌웨어는 자동 업데이트가 꺼져 있습니다. 웹 UI에서 수동 업데이트를 하면 경고 후 공식 펌웨어로 바뀌며,
  그 뒤에는 `custom/update.sh` → `custom/deploy.sh -i`로 다시 설치하면 됩니다.

## 수정 사항과 파일

### 1. 터치 입력 + USB 터치스크린
| 파일 | 내용 |
|---|---|
| `ui/src/hooks/useTouch.ts` (새 파일) | 터치 제스처. 터치스크린이 꺼져 있으면 마우스로 변환, 켜져 있으면 손가락 좌표를 그대로 전송 |
| `ui/src/hooks/useMouse.ts` | 좌표 변환을 `calcAbsMousePosition`으로 분리 |
| `ui/src/components/WebRTCVideo.tsx` | 터치 이벤트 연결, `touch-none` 등. 터치패드 핀치(Ctrl+휠)로 브라우저가 확대되지 않게 막음 (별도 커밋 `fix(ui): keep touchpad pinch ...`) |
| `ui/src/hooks/hidRpc.ts`, `useHidRpc.ts` | `TouchscreenReport`(0x0A) 메시지 |
| `ui/src/hooks/stores.ts`, `components/UsbDeviceSetting.tsx`, `components/ActionBar.tsx` | 터치스크린 설정 상태와 체크박스 |
| `internal/usbgadget/ffs_touchscreen.go` (새 파일) | FunctionFS 터치스크린 (ep0 요청 응답, 입력 보고 전송) |
| `internal/usbgadget/hid_touchscreen.go` (새 파일) | 터치스크린 HID 설명 데이터와 보고 형식. 동시 터치 수는 `TouchscreenMaxContacts`(5, 최대 10)와 UI의 `MAX_TOUCH_CONTACTS`를 함께 바꿈 |
| `internal/usbgadget/config.go`, `usbgadget.go` | 장치 목록에 `touchscreen` 추가, 바인딩 전 FunctionFS 준비 |
| `internal/hidrpc/*.go`, `hidrpc.go`, `usb.go`, `jsonrpc.go` | 메시지 해석과 전달, 설정 RPC |

### 2. 콘솔 상단 바
`ui/src/components/ActionBar.tsx`, `ui/src/components/ResolutionButton.tsx` (새 파일)
— Extension, KVM Terminal 버튼 제거 / 전체화면 버튼 항상 표시 / Resolution 분할 버튼
(누르면 창에 맞추기 1회, 화살표를 누르면 붙여넣기처럼 카드 패널이 열림: 해상도 안전모드(기본 EDID 적용 + 모든
해상도 항목 잠금), 기준 해상도 라디오 1280x720/1600x900/1920x1080, Auto fit, Fit now, 현재 EDID 표시.
EDID 프리셋 목록은 패널에서 빠졌고 비디오 설정에만 있음.
Auto fit을 켜면
10초마다 영역 비율을 보고 마지막으로 맞춘 비율과 3% 이상 다르면 다시 맞춤. 브라우저별 설정.
"Auto fit in fullscreen"은 전체화면일 때 대신 적용(기기 회전 대비 10초마다 확인). 이것만 켜져 있으면
전체화면을 나올 때 이전 EDID로 되돌림. 바뀐 게 없으면 장치에 명령을 보내지 않음(setFitEDID도 같은 EDID면 건너뜀))

창에 맞추기: `edid_fit.go`(RPC `setFitEDID`) + `internal/edidfit`(해상도 선택, CVT 표준 타이밍, EDID 생성).
크기는 선택한 기준 해상도의 화소 수 근처로 두고 비율만 영역에 맞춤. 1920x1080처럼 CVT 표준이 140MHz를 넘으면
저블랭킹으로 대체(같은 부족량 보정 적용). 세로는 8의 배수.
EDID 변경은 장치에서 `edidfit.ChangeLimiter`로 한 번에 하나씩, 직전 변경 후 10초 안의 요청은 보류했다가
마지막 것만 적용(원본 `rpcSetEDID` 본문은 `applyEDID`로 이름만 바꿈). 다른 기기 접속 팝업(/other-session)이
떠 있는 탭은 해상도 요청을 보내지 않음. `edid_fit.go`의 SyncMaster 1792x896 템플릿을 바탕으로 제품 코드 `0x0010`, 일련번호 = 가로<<16 | 세로로 만듦.

### 3. EDID 프리셋 (원복됨)
`edid_presets.go`는 원본과 같음(커밋 `revert: restore edid_presets.go` 참고). 창에 맞추기 템플릿
(Samsung SyncMaster 1792x896 EDID)은 `edid_fit.go`의 `fitTemplateEDID`에 들어 있음.

### 4. 자동 업데이트 비활성화
| 파일 | 내용 |
|---|---|
| `custom_firmware.go` (새 파일) | `customFirmware` 표시, 시작할 때 `auto_update_enabled=false` 저장 |
| `main.go` | 자동 업데이트 루프가 동작하지 않음 |
| `jsonrpc.go` | 자동 업데이트를 켜는 요청 거부 |
| `mqtt_commands.go` | MQTT 업데이트 명령 거부 (경고를 띄울 수 없어서) |
| `ui/src/routes/devices.$id.settings.general._index.tsx` | 자동 업데이트 체크박스 비활성화와 안내 문구 |
| `ui/src/routes/devices.$id.settings.general.update.tsx` | 수동 업데이트 전에 경고와 확인 창 |

### 5. 검은색 다크 테마
`ui/src/index.css` — 다크 모드일 때만 slate/gray 색상표를 무채색으로 바꿔 끼움(클래스는 그대로) /
`ui/index.html` — 로딩 화면(skeleton)의 다크 색상

### 6. 장치 화면에 접속자 IP 표시
장치 자체 화면에서 원래 장치 IPv4가 있던 큰 글씨 자리에 콘솔을 쓰는 브라우저의 IP를 표시(없으면 `No session`, 클라우드 접속에서 IP를 알 수 없으면 `Cloud`), 오른쪽 위 `N active`(접속 수) 자리에는 장치 MAC과 IPv4를 두 줄로 오른쪽 정렬해 표시(가운데의 MAC 줄은 숨김). 가운데 영역은 No Network 화면처럼 가운데 정렬하고 주소 위에 상태 아이콘(Material Symbols Outlined `pause_circle` = 접속 없음, `lan` = 로컬 접속, `cloud` = 클라우드 접속; 굵기 300, 32px, 글자보다 약간 어두운 #d0d0d0)을 표시. MAC이 들어가도록 로고 이미지(116x32)에서 왼쪽 아이콘(32x32)만 보이게 잘라 "JetKVM" 글자를 없애고(원본의 `transform_width/height` 확장은 0으로), 두 줄 머리글 때문에 생기는 스크롤바를 끔.
`session_ip.go`(표시 문구, 클라우드 접속은 선택된 ICE 후보에서 IP를 얻음) / `display.go`(`cloud_status_label` ↔ `home_info_ipv4_addr`) / `webrtc.go`(`clientIP` 필드) / `cloud.go`(`handleSessionRequest`에서 로컬 접속의 IP 전달) / `internal/native/eez/src/ui/screens.c`(머리글 라벨 오른쪽 정렬, 로고, 스크롤바, 가운데 정렬과 `home_session_icon_*`), `screens.h`, `images.c`/`images.h`, `images/ui_image_session_*.c`(32x32 아이콘, 스크립트로 그림)

## 충돌이 나기 쉬운 곳

| 위치 | 이유 / 해결 요령 |
|---|---|
| `ui/src/components/ActionBar.tsx` | 원본에서 자주 바뀜. 원본 버전을 받아들인 뒤 위 2번 변경과 터치스크린 상태 코드(`setUsbTouchscreenEnabled`)를 다시 넣기 |
| `ui/src/components/WebRTCVideo.tsx` | 마우스 이벤트 연결부(`setMouseModeEventListeners`)와 `<video>` className |
| `ui/localization/messages/en.json` | 키를 알파벳 순서로 다시 넣으면 됨 |
| `internal/hidrpc/hidrpc.go` | **원본이 0x0A 메시지 번호를 새로 쓰기 시작하면 번호를 바꿔야 함** (Go와 `ui/src/hooks/hidRpc.ts` 둘 다) |
| `internal/usbgadget/config.go` | 원본이 USB 장치를 추가하면 `order: 1004`, `ffs.touchscreen` 이름이 겹치지 않는지 확인 |
| `internal/native/eez/src/ui/screens.c` | EEZ Studio가 만드는 파일. 원본이 화면을 다시 만들면 `// only the icon`, `// these widen`, `// session state centered`, `// two lines`, `// the two-line header` 주석이 붙은 줄을 다시 넣기 |
| `jsonrpc.go`의 `applyEDID` | 원본 `rpcSetEDID` 본문을 이름만 바꾼 것. 원본이 이 함수를 고치면 `applyEDID`에 반영하고 `rpcSetEDID`는 `edid_fit.go` 것을 유지 |
| `main.go` 자동 업데이트 루프, `ui/.../general.update.tsx` | 원본이 업데이트 흐름을 바꾸면 `customFirmware` 조건과 확인 창(`UpdateAvailableState`)을 다시 넣기. **새 업데이트 경로(예: 클라우드 명령)가 생기면 그것도 막아야 함** |

## 알아둘 하드웨어 사항

- USB HID 장치(`f_hid`)는 커널 제한으로 4개까지라, 터치스크린은 FunctionFS로 구현했습니다.
- FunctionFS ep0를 blocking으로 읽으면 USB 재연결 때 커널이 멈춥니다(poll + non-blocking 필수).
- 앱이 종료되면 FunctionFS가 닫히면서 커널이 USB 전체를 잠깐 해제합니다. 앱이 다시 시작되면 복구됩니다.
- HDMI 입력 칩 TC358743은 줄을 CSI-2로 고정 속도(약 155 Mpx/s)로 내보냅니다. 픽셀 클럭이 너무 낮으면
  줄 끝이 잘려 초록색 줄무늬가 생깁니다(`csi size err`). 한 줄 부족량 = 가로 × (1 − 픽셀클럭/155MHz)을
  **200픽셀 이하**로 유지해야 합니다(1792x896@132.75MHz=258 정상, 1792x800@117MHz=436 깨짐).
  좌우로 긴(세로 줄이 적은) 해상도와 저블랭킹 타이밍이 특히 위험합니다.
- EDID를 새로 만들 때는 `internal/edidfit.Mode(w, h)`를 쓰세요: CVT 표준 블랭킹에 백 포치를 늘려
  위 조건을 맞추고, 세로 1200·픽셀 클럭 140MHz·수평 81kHz 이하를 지킵니다.
