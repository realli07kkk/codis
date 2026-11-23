package utils

import (
	"fmt"
	"net/http"

	"github.com/CodisLabs/codis/pkg/shake/config"
	"github.com/CodisLabs/codis/pkg/shake/log"
)

func SetPprofPort() {
	// pprof_port
	if config.Opt.Advanced.PprofPort != 0 {
		go func() {
			err := http.ListenAndServe(fmt.Sprintf("localhost:%d", config.Opt.Advanced.PprofPort), nil)
			if err != nil {
				log.Panicf("%v", err)
			}
		}()
		log.Infof("pprof information: http://localhost:%d/debug/pprof/", config.Opt.Advanced.PprofPort)
	} else {
		log.Infof("not set pprof port")
	}
}
