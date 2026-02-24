package repository_test

import (
	"github.com/streamingfast/logging"
)

var zlogTest, _ = logging.PackageLogger("repository_test", "github.com/graphprotocol/substreams-data-service/provider/repository/tests")

func init() {
	logging.InstantiateLoggers()
}
