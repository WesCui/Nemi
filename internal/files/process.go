package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"nemi/internal/domain"
)

type ParseRequest struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}
type ParseResponse struct {
	Content domain.FileContent `json:"content"`
	Error   string             `json:"error,omitempty"`
}

var parserSlots = make(chan struct{}, 2)

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("output too large")
	}
	return b.Buffer.Write(p)
}

// The parser runs outside API/worker processes. GOMEMLIMIT is a soft Go runtime
// limit, not a hard OS sandbox; timeout, ZIP/page/content limits also apply.
func ParseProcess(ctx context.Context, name string, data []byte) (domain.FileContent, error) {
	select {
	case parserSlots <- struct{}{}:
		defer func() { <-parserSlots }()
	case <-ctx.Done():
		return domain.FileContent{}, errors.New("FILE_PARSER_UNAVAILABLE")
	}
	if _, err := MIME(name); err != nil {
		return domain.FileContent{}, err
	}
	if len(data) > MaxBytes {
		return domain.FileContent{}, errors.New("FILE_TOO_LARGE")
	}
	self, err := os.Executable()
	if err != nil {
		return domain.FileContent{}, errors.New("FILE_PARSER_UNAVAILABLE")
	}
	binary := "file-parser"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	parserPath := filepath.Join(filepath.Dir(self), binary)
	if _, err := os.Stat(parserPath); err != nil {
		// go run places its executable in a temporary directory. Development
		// builds keep the helper in the repository's fixed binary directory.
		parserPath = filepath.Join(".cache", "bin", binary)
	}
	cmd := exec.CommandContext(ctx, parserPath)
	cmd.Env = []string{"GOMEMLIMIT=128MiB", "GOMAXPROCS=1"}
	for _, k := range []string{"SystemRoot", "TEMP", "TMP", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	input, _ := json.Marshal(ParseRequest{Name: name, Data: data})
	cmd.Stdin = bytes.NewReader(input)
	var output boundedBuffer
	output.max = MaxContent + 4096
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return domain.FileContent{}, errors.New("FILE_PARSER_UNAVAILABLE")
	}
	var reply ParseResponse
	if json.Unmarshal(output.Bytes(), &reply) != nil {
		return domain.FileContent{}, errors.New("FILE_PARSER_UNAVAILABLE")
	}
	if reply.Error != "" {
		return domain.FileContent{}, errors.New(reply.Error)
	}
	return reply.Content, Validate(reply.Content)
}
