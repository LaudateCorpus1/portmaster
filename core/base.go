package core

import (
	"errors"
	"flag"

	"github.com/safing/portbase/config"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/database/dbmodule"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"

	"github.com/safing/portmaster/core/structure"
)

var (
	dataDir     string
	databaseDir string
)

func init() {
	flag.StringVar(&dataDir, "data", "", "set data directory")
	flag.StringVar(&databaseDir, "db", "", "alias to --data (deprecated)")

	modules.Register("base", prepBase, nil, nil, "info")
}

func prepBase() error {
	// backwards compatibility
	if dataDir == "" {
		dataDir = databaseDir
	}

	// check data dir
	if dataDir == "" {
		return errors.New("please set the data directory using --data=/path/to/data/dir")
	}

	// initialize structure
	err := structure.Initialize(dataDir, 0755)
	if err != nil {
		return err
	}

	// set data location
	dbmodule.SetDatabaseLocation("", structure.Root())
	config.SetDataRoot(structure.Root())

	// init config
	logFlagOverrides()
	err = registerConfig()
	if err != nil {
		return err
	}

	// set api listen address
	api.SetDefaultAPIListenAddress("127.0.0.1:817")

	// set notification persistence
	notifications.SetPersistenceBasePath("core:notifications")

	return nil
}
