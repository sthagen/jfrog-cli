package xray

import (
	"errors"
	"github.com/codegangsta/cli"
	corecommon "github.com/jfrog/jfrog-cli-core/docs/common"
	"github.com/jfrog/jfrog-cli-core/xray/commands"
	"github.com/jfrog/jfrog-cli/docs/common"
	"github.com/jfrog/jfrog-cli/docs/xray/offlineupdate"
	"github.com/jfrog/jfrog-cli/utils/cliutils"
	"github.com/jfrog/jfrog-client-go/utils/errorutils"
	"time"
)

const DATE_FORMAT = "2006-01-02"

func GetCommands() []cli.Command {
	return []cli.Command{
		{
			Name:         "offline-update",
			Usage:        offlineupdate.Description,
			HelpName:     corecommon.CreateUsage("xr offline-update", offlineupdate.Description, offlineupdate.Usage),
			ArgsUsage:    common.CreateEnvVars(),
			Flags:        cliutils.GetCommandFlags(cliutils.OfflineUpdate),
			Aliases:      []string{"ou"},
			BashComplete: corecommon.CreateBashCompletionFunc(),
			Action:       offlineUpdates,
		},
	}
}

func getOfflineUpdatesFlag(c *cli.Context) (flags *commands.OfflineUpdatesFlags, err error) {
	flags = new(commands.OfflineUpdatesFlags)
	flags.Version = c.String("version")
	flags.License = c.String("license-id")
	flags.Target = c.String("target")
	if len(flags.License) < 1 {
		return nil, errors.New("The --license-id option is mandatory.")
	}
	from := c.String("from")
	to := c.String("to")
	if len(to) > 0 && len(from) < 1 {
		return nil, errors.New("The --from option is mandatory, when the --to option is sent.")
	}
	if len(from) > 0 && len(to) < 1 {
		return nil, errors.New("The --to option is mandatory, when the --from option is sent.")
	}
	if len(from) > 0 && len(to) > 0 {
		flags.From, err = dateToMilliseconds(from)
		errorutils.CheckError(err)
		if err != nil {
			return
		}
		flags.To, err = dateToMilliseconds(to)
		errorutils.CheckError(err)
	}
	return
}

func dateToMilliseconds(date string) (dateInMillisecond int64, err error) {
	t, err := time.Parse(DATE_FORMAT, date)
	if err != nil {
		errorutils.CheckError(err)
		return
	}
	dateInMillisecond = t.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
	return
}

func offlineUpdates(c *cli.Context) error {
	offlineUpdateFlags, err := getOfflineUpdatesFlag(c)
	if err != nil {
		return err
	}

	return commands.OfflineUpdate(offlineUpdateFlags)
}
