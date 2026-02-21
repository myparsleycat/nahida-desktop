## start-process
dll-injector start-process \
  --injector-lib-path <인젝터DLL_경로> \
  --exe-path <실행할EXE_경로> \
  [--work-dir <작업디렉토리>] \
  [--start-args <실행인수>]

## 프로세스 시작 및 dll 주입 open-process
dll-injector open-process \
  --injector-lib-path <인젝터DLL_경로> \
  --start-method <NATIVE | SHELL | MANUAL> \
  [--exe-path <실행할EXE_경로>] \
  [--work-dir <작업디렉토리>] \
  [--start-args <실행인수1> <실행인수2> ...] \
  [--process-flags <생성플래그>] \
  [--process-name <대상프로세스명>] \
  [--dll-paths <주입할DLL_경로1> <경로2> ...] \
  [--cmd <CMD명령어>] \
  [--inject-timeout <초, 기본값:15>]

## 훅킹 및 대기 hook-and-wait
dll-injector hook-and-wait \
  --injector-lib-path <인젝터DLL_경로> \
  --dll-path <로드할DLL_경로> \
  --target-process <대상프로세스명> \
  [--timeout <초, 기본값:15>]

## 실행 중인 프로세스에 라이브러리 주입 inject-libraries
dll-injector inject-libraries \
  --dll-paths <주입할DLL_경로1> <경로2> ... \
  [--process-name <대상프로세스명>] \
  [--pid <대상PID>] \
  [--timeout <초, 기본값:15>]