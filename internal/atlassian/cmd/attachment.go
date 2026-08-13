package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/output"
)

var attachmentCmd = &cobra.Command{
	Use:     "attachment",
	Aliases: []string{"attach"},
	Short:   "Jira issue attachments",
}

var attachmentOut string

var attachmentListCmd = &cobra.Command{
	Use:   "list <issueKey>",
	Short: "List attachments",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		as, err := svc.Client.Attachments(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(as)
		}
		rows := make([][]string, len(as))
		for i, a := range as {
			rows[i] = []string{a.ID, a.Filename, strconv.FormatInt(a.Size, 10), a.MimeType, output.UserName(a.Author), shortTime(a.Created)}
		}
		return p.Table([]string{"ID", "Filename", "Size", "Type", "Author", "Created"}, rows)
	},
}

var attachmentDownloadCmd = &cobra.Command{
	Use:   "download <attachmentId>",
	Short: "Download an attachment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		meta, err := svc.Client.AttachmentMeta(c, args[0])
		if err != nil {
			return err
		}
		data, _, err := svc.Client.DownloadAttachment(c, meta.Content)
		if err != nil {
			return err
		}
		out := attachmentOut
		if out == "" {
			out = meta.Filename
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		printer().Msg("wrote %s (%d bytes)", out, len(data))
		return nil
	},
}

var attachmentUploadCmd = &cobra.Command{
	Use:   "upload <issueKey> <file>",
	Short: "Upload a file as an attachment",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		c, cancel := ctx()
		defer cancel()
		as, err := svc.Client.UploadAttachment(c, args[0], filepath.Base(args[1]), f)
		if err != nil {
			return err
		}
		if len(as) == 0 {
			return fmt.Errorf("upload returned no attachment")
		}
		printer().Msg("uploaded %s (id %s)", as[0].Filename, as[0].ID)
		return nil
	},
}

var attachmentDeleteCmd = &cobra.Command{
	Use:   "delete <attachmentId>",
	Short: "Delete an attachment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		if err := svc.Client.DeleteAttachment(c, args[0]); err != nil {
			return err
		}
		printer().Msg("deleted attachment %s", args[0])
		return nil
	},
}

func init() {
	attachmentDownloadCmd.Flags().StringVarP(&attachmentOut, "output", "o", "", "output path (default: original filename)")
	attachmentCmd.AddCommand(attachmentListCmd, attachmentDownloadCmd, attachmentUploadCmd, attachmentDeleteCmd)
	rootCmd.AddCommand(attachmentCmd)
}
