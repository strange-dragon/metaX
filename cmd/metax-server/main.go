// The metaX server entrypoint. The server, SDK engine, and native providers
// are all part of this repository, so no upstream checkout or build-time
// patching is required.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/strange-dragon/metaX/buildinfo"
	"github.com/strange-dragon/metaX/cmd"
	"github.com/strange-dragon/metaX/engine"
)

func showVersionAndExit() {
	fmt.Println(buildinfo.BuildString())
	os.Exit(0)
}

func main() {
	if _, isSet := os.LookupEnv("VERSION"); cmd.Config.VersionFlag &&
		!isSet /* NOTE: ignore this flag if ENV contains VERSION variable. */ {
		showVersionAndExit()
	}

	var (
		addr = net.JoinHostPort(
			cmd.Config.Bind,
			cmd.Config.Port)
		router = cmd.Router(engine.DefaultEngineName)
	)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
