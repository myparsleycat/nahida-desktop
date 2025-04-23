package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unsafe"
)

// 요청 구조체
type HashRequest struct {
	FilePath string `json:"filePath"`
}

// 응답 구조체
type HashResponse struct {
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

//export CalculateSHA256
func CalculateSHA256(inputJSON *C.char) *C.char {
	// Go string으로 변환
	input := C.GoString(inputJSON)

	// JSON 파싱
	var request HashRequest
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		response := HashResponse{Error: fmt.Sprintf("JSON 파싱 오류: %v", err)}
		jsonResponse, _ := json.Marshal(response)
		return C.CString(string(jsonResponse))
	}

	// 파일 열기
	file, err := os.Open(request.FilePath)
	if err != nil {
		response := HashResponse{Error: fmt.Sprintf("파일 열기 오류: %v", err)}
		jsonResponse, _ := json.Marshal(response)
		return C.CString(string(jsonResponse))
	}
	defer file.Close()

	// SHA256 해시 계산
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		response := HashResponse{Error: fmt.Sprintf("해시 계산 오류: %v", err)}
		jsonResponse, _ := json.Marshal(response)
		return C.CString(string(jsonResponse))
	}

	// 해시 결과 변환
	hashBytes := hasher.Sum(nil)
	hashString := hex.EncodeToString(hashBytes)

	// 응답 구성 및 반환
	response := HashResponse{Hash: hashString}
	jsonResponse, _ := json.Marshal(response)
	return C.CString(string(jsonResponse))
}

// 메모리 해제 함수 (Node.js에서 호출)
//
//export FreeMemory
func FreeMemory(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {
	// 빈 main 함수 - 이 코드는 공유 라이브러리로 사용될 예정
}
