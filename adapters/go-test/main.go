// Command go-test runs `go test -json` and prints a lauren-stats line.
// It does not decide lauren's gate. The check's require_executed switch does.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		args = []string{"./..."}
	}
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var lines []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		lines = append(lines, line)
		fmt.Println(line)
	}
	waitErr := cmd.Wait()
	tally := tallyEvents(lines)
	fmt.Printf("lauren-stats executed=%d skipped=%d\n", tally.executed, tally.skipped)
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, waitErr)
		os.Exit(1)
	}
}
