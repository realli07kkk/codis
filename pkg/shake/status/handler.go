package status

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/CodisLabs/codis/pkg/shake/config"
	"github.com/CodisLabs/codis/pkg/shake/log"
)

func Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Content-Type", "application/json")

	bytesChannel := make(chan []byte, 1)

	ch <- func() {
		stat.Consistent = theReader.StatusConsistent() && theWriter.StatusConsistent()
		jsonBytes, err := json.Marshal(stat)
		if err != nil {
			log.Warnf("marshal status info failed, err=[%v]", err)
			bytesChannel <- []byte(fmt.Sprintf(`{"error": "%v"}`, err))
			return
		}
		bytesChannel <- jsonBytes
	}

	select {
	case bytes := <-bytesChannel:
		_, err := w.Write(bytes)
		if err != nil {
			log.Warnf("write status info failed, err=[%v]", err)
		}
	case <-time.After(time.Second * 3):
		log.Warnf("write status info timeout")
		w.WriteHeader(http.StatusRequestTimeout)
	}
}

// setStatusPort starts the status HTTP server. codis-shake binds it to the
// loopback interface only: the port is chosen by codis-dashboard, which polls
// it from the same host, and the status must not be reachable from peers.
func setStatusPort() {
	if config.Opt.Advanced.StatusPort != 0 {
		go func() {
			addr := fmt.Sprintf("127.0.0.1:%d", config.Opt.Advanced.StatusPort)
			if err := http.ListenAndServe(addr, http.HandlerFunc(Handler)); err != nil {
				log.Panicf("%v", err)
			}
		}()
		log.Infof("status information: http://127.0.0.1:%v", config.Opt.Advanced.StatusPort)
	} else {
		log.Infof("not set status port")
	}
}
