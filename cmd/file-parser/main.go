package main

import (
	"encoding/json"
	"io"
	"os"

	"nemi/internal/files"
)

func main() {
	response := files.ParseResponse{Error: "FILE_INVALID"}
	defer func() {
		if recover() != nil {
			response = files.ParseResponse{Error: "FILE_INVALID"}
		}
		json.NewEncoder(os.Stdout).Encode(response)
	}()
	var request files.ParseRequest
	if json.NewDecoder(io.LimitReader(os.Stdin, (files.MaxBytes*4/3)+4096)).Decode(&request) != nil {
		return
	}
	c, err := files.Parse(request.Name, request.Data)
	if err != nil {
		response.Error = err.Error()
		return
	}
	response = files.ParseResponse{Content: c}
}
