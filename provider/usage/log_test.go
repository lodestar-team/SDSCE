package usage_test

import (
	"github.com/streamingfast/logging"
)

var zlogTest, _ = logging.PackageLogger("usage_test", "github.com/graphprotocol/substreams-data-service/provider/usage/tests")

func init() {
	logging.InstantiateLoggers()
}
