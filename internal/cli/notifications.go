package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newNotificationChannelsCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notification-channels",
		Aliases: []string{"notifs"},
		Short:   "Query and manage per-monitor notification channels",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <monitor-id>",
		Short: "List notification channels for a monitor",
		Example: `  pulsetic-cli notifs list 4172`,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/monitors/%d/notification-channels", id)
			_, err = rc.callAndRecord(c.Context(), "notification_channels.list", "GET", path, nil)
			return err
		},
	})

	// Each channel type has its own POST endpoint with a different body field.
	channelTypes := []struct {
		name     string
		path     string
		example  string
	}{
		{"email", "email", `'{"email":"user@example.com"}'`},
		{"phone", "phone-number", `'{"phone_number":"+1234567890","sms":true,"calls":false}'`},
		{"webhook", "webhook", `'{"webhook":"https://hooks.example.com/alert"}'`},
		{"slack", "slack-webhook", `'{"webhook_url":"https://hooks.slack.com/..."}'`},
		{"discord", "discord-webhook", `'{"webhook":"https://discord.com/api/webhooks/..."}'`},
		{"ms-teams", "ms-teams-webhook", `'{"webhook":"https://outlook.office.com/webhook/..."}'`},
		{"signl4", "signl4", `'{"signl4_webhook":"https://connect.signl4.com/..."}'`},
	}

	for _, ct := range channelTypes {
		ct := ct // capture
		var data string
		addCmd := &cobra.Command{
			Use:     fmt.Sprintf("add-%s <monitor-id>", ct.name),
			Short:   fmt.Sprintf("Add a %s notification channel to a monitor", ct.name),
			Long:    fmt.Sprintf("Pass JSON via --data, e.g. --data %s", ct.example),
			Example: fmt.Sprintf("  pulsetic-cli notifs add-%s 4172 --data %s", ct.name, ct.example),
			Args:    cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, err := parseID(args[0])
				if err != nil {
					return err
				}
				body, err := readJSONInput(data, true)
				if err != nil {
					return err
				}
				rc, err := opts.prepare(version, c.OutOrStdout())
				if err != nil {
					return err
				}
				defer rc.close()
				path := fmt.Sprintf("/monitors/%d/notification-channels/%s", id, ct.path)
				command := fmt.Sprintf("notification_channels.add_%s", ct.name)
				_, err = rc.callAndRecordWithBody(c.Context(), command, "POST", path, nil, body)
				return err
			},
		}
		addCmd.Flags().StringVar(&data, "data", "", `JSON body (or - for stdin)`)
		cmd.AddCommand(addCmd)
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <channel-id>",
		Short:   "Delete a notification channel",
		Example: `  pulsetic-cli notifs delete 42`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/notification-channels/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "notification_channels.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	return cmd
}
