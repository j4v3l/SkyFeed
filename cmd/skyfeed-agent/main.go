// Command skyfeed-agent reserves the outbound LAN-agent binary boundary for a
// future cloud deployment. Direct local HTTP remains the supported source.
package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "skyfeed-agent is not enabled; use the direct private HTTP source")
	os.Exit(2)
}
