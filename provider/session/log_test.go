package session_test

import (
	"github.com/streamingfast/logging"
)

var zlogTest, _ = logging.PackageLogger("session_test", "github.com/graphprotocol/substreams-data-service/provider/session/tests")

func init() {
	logging.InstantiateLoggers()
}
