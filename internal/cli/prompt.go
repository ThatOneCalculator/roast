package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptYesDefault(w io.Writer, r io.Reader, question string) (bool, error) {
	fmt.Fprintf(w, "%s [Y/n] ", question)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if err == io.EOF && answer == "" {
		return false, nil
	}
	switch answer {
	case "", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
