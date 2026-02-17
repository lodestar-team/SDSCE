package auth_test

import (
	"github.com/streamingfast/logging"
)

var zlogTest, _ = logging.PackageLogger("auth_test", "github.com/graphprotocol/substreams-data-service/provider/auth/tests")

func init() {
	logging.InstantiateLoggers()
}
