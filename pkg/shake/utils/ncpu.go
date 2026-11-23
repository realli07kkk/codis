package utils

import (
	"runtime"

	"github.com/CodisLabs/codis/pkg/shake/config"
	"github.com/CodisLabs/codis/pkg/shake/log"
)

func SetNcpu() {
	if config.Opt.Advanced.Ncpu != 0 {
		log.Infof("set ncpu to %d", config.Opt.Advanced.Ncpu)
		runtime.GOMAXPROCS(config.Opt.Advanced.Ncpu)
		log.Infof("set GOMAXPROCS to %v", config.Opt.Advanced.Ncpu)
	} else {
		log.Infof("GOMAXPROCS defaults to the value of runtime.NumCPU [%v]", runtime.NumCPU())
	}
}
