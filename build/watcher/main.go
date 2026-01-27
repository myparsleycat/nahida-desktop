package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// nput command from nahida desktop
type Command struct {
	Action    string `json:"action"` // "watch" or "unwatch"
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// output event to nahida desktop
type Event struct {
	Event string `json:"event"` // "add", "change", "unlink", "error"
	Path  string `json:"path"`
	Info  string `json:"info,omitempty"`
}

var (
	watchers = make(map[string]chan struct{})
	mu       sync.Mutex
	outputMu sync.Mutex
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		var cmd Command
		if err := json.Unmarshal(line, &cmd); err != nil {
			sendError("Invalid JSON command: " + err.Error())
			continue
		}

		handleCommand(cmd)
	}
}

func handleCommand(cmd Command) {
	mu.Lock()
	defer mu.Unlock()

	switch cmd.Action {
	case "watch":
		if _, exists := watchers[cmd.Path]; exists {
			// Already watching
			return
		}

		stopCh := make(chan struct{})
		watchers[cmd.Path] = stopCh
		go watchDirectory(cmd.Path, cmd.Recursive, stopCh)

	case "unwatch":
		if stopCh, exists := watchers[cmd.Path]; exists {
			close(stopCh)
			delete(watchers, cmd.Path)
		}
	}
}

func sendEvent(evt Event) {
	outputMu.Lock()
	defer outputMu.Unlock()

	data, _ := json.Marshal(evt)
	fmt.Println(string(data))
}

func sendError(msg string) {
	sendEvent(Event{
		Event: "error",
		Info:  msg,
	})
}

const (
	// FILE_NOTIFY_CHANGE_* flags
	notifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
		windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_ATTRIBUTES |
		windows.FILE_NOTIFY_CHANGE_SIZE |
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE |
		windows.FILE_NOTIFY_CHANGE_CREATION
)

func watchDirectory(pathStr string, recursive bool, stopCh chan struct{}) {
	pathPtr, err := windows.UTF16PtrFromString(pathStr)
	if err != nil {
		sendError(fmt.Sprintf("Invalid path %s: %v", pathStr, err))
		return
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		sendError(fmt.Sprintf("Failed to open directory %s: %v", pathStr, err))
		return
	}
	defer windows.CloseHandle(handle)

	overlapped := &windows.Overlapped{}
	hEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		sendError(fmt.Sprintf("Failed to create event: %v", err))
		return
	}
	defer windows.CloseHandle(hEvent)
	overlapped.HEvent = hEvent

	buffer := make([]byte, 65536) // 64KB buffer

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		var bytesReturned uint32
		err = windows.ReadDirectoryChanges(
			handle,
			&buffer[0],
			uint32(len(buffer)),
			recursive,
			notifyFilter,
			&bytesReturned,
			overlapped,
			0,
		)

		if err != nil {
			// ERROR_IO_PENDING is normal for overlapped
			if err != syscall.ERROR_IO_PENDING {
				sendError(fmt.Sprintf("ReadDirectoryChanges failed: %v", err))
				return
			}
		}

		stopEvent, _ := windows.CreateEvent(nil, 1, 0, nil)
		defer windows.CloseHandle(stopEvent)

		go func() {
			<-stopCh
			windows.SetEvent(stopEvent)
		}()

		events := []windows.Handle{hEvent, stopEvent}
		waitResult, err := windows.WaitForMultipleObjects(events, false, windows.INFINITE)
		if err != nil {
			sendError(fmt.Sprintf("WaitForMultipleObjects failed: %v", err))
			return
		}

		// WAIT_OBJECT_0 is hEvent (index 0)
		// WAIT_OBJECT_0 + 1 is stopEvent (index 1)
		if waitResult == windows.WAIT_OBJECT_0+1 {
			// Stop signal
			// Cancel I/O
			windows.CancelIo(handle)
			return
		}

		var nbytes uint32
		err = windows.GetOverlappedResult(handle, overlapped, &nbytes, false)
		if err != nil {
			sendError(fmt.Sprintf("GetOverlappedResult failed: %v", err))
			return
		}

		processBuffer(buffer[:nbytes], pathStr)
	}
}

func processBuffer(buffer []byte, rootPath string) {
	if len(buffer) == 0 {
		return
	}

	var offset uint32
	for {
		entry := (*windows.FileNotifyInformation)(unsafe.Pointer(&buffer[offset]))

		// filename is [FileNameLength/2]uint16
		// Pointer arithmetic to get the name slice
		namePtr := (*[1 << 30]uint16)(unsafe.Pointer(&entry.FileName))
		nameLen := entry.FileNameLength / 2
		nameSlice := namePtr[:nameLen:nameLen]

		fileName := windows.UTF16ToString(nameSlice)
		fullPath := filepath.Join(rootPath, fileName)

		var eventType string
		switch entry.Action {
		case windows.FILE_ACTION_ADDED, windows.FILE_ACTION_RENAMED_NEW_NAME:
			eventType = "add"
		case windows.FILE_ACTION_REMOVED, windows.FILE_ACTION_RENAMED_OLD_NAME:
			eventType = "unlink"
		case windows.FILE_ACTION_MODIFIED:
			eventType = "change"
		}

		if eventType != "" {
			sendEvent(Event{
				Event: eventType,
				Path:  fullPath,
			})
		}

		if entry.NextEntryOffset == 0 {
			break
		}
		offset += entry.NextEntryOffset
	}
}
