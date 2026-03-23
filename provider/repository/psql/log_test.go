package psql

import (
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog, _ = logging.PackageLogger("psql-test", "github.com/graphprotocol/substreams-data-service/provider/repository/psql")

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.PanicLevel))
}
