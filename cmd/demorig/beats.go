package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newBeatsCommand locates every beat of a storyboard in a raw take: one row per beat with its
// id, its start in seconds into the take and its title, or the same as JSON.
func newBeatsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "beats <storyboard.yaml> <take.mp4> <session.json>",
		Short: "Print where each beat of the storyboard starts in a take",
		Long: "beats reads the take's saved session, pins its first prompt to the storyboard's\n" +
			"align.first_prompt_at and prints the second each beat starts at. first-paint is found\n" +
			"by ffmpeg's scene detection and end by ffprobe.",
		Args: cobra.ExactArgs(3),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			times, err := locateBeats(cmd, args[0], args[1], args[2])
			if err != nil {
				return err
			}
			if asJSON {
				return writeBeatsJSON(cmd.OutOrStdout(), times)
			}
			return writeBeatsTable(cmd.OutOrStdout(), times)
		}),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the rows as a JSON array")
	return cmd
}

// locateBeats loads the storyboard and the session, probes the take, and resolves the beats.
func locateBeats(cmd *cobra.Command, storyboard, take, sessionPath string) ([]BeatTime, error) {
	board, err := Load(storyboard)
	if err != nil {
		return nil, err
	}
	entries, err := loadEntries(sessionPath)
	if err != nil {
		return nil, err
	}
	duration, err := takeDuration(cmd.Context(), take)
	if err != nil {
		return nil, err
	}
	painter := ffmpegFirstPainter{Take: take, Threshold: board.Align.SceneThreshold}
	return resolveBeats(cmd.Context(), board, entries, painter, duration)
}

// beatRow is the JSON form of one resolved beat: seconds into the take, and the index of the
// transcript entry a session anchor selected (-1 for a video or beat anchor).
type beatRow struct {
	ID    int     `json:"id"`
	Title string  `json:"title"`
	At    float64 `json:"at"`
	Entry int     `json:"entry"`
}

// writeBeatsJSON prints the rows as one JSON array.
func writeBeatsJSON(w io.Writer, times []BeatTime) error {
	rows := make([]beatRow, 0, len(times))
	for _, t := range times {
		rows = append(rows, beatRow{ID: t.ID, Title: t.Title, At: t.At.Seconds(), Entry: t.Index})
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rows)
}

// writeBeatsTable prints one aligned row per beat: id, seconds into the take, title.
func writeBeatsTable(w io.Writer, times []BeatTime) error {
	for _, t := range times {
		if _, err := fmt.Fprintf(w, "%3d  %8.3fs  %s\n", t.ID, t.At.Seconds(), t.Title); err != nil {
			return err
		}
	}
	return nil
}
