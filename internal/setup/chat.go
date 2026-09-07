package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// bridgeBotLocalpart is the mautrix-meta Instagram bridge's own bot account,
// fixed by the bridge itself (docs/self-host.md "四、登入 Instagram").
const bridgeBotLocalpart = "instagrambot"

// findOrCreateBridgeDM returns the 1:1 room with the bridge bot, reusing one
// from a previous run instead of opening a duplicate.
func findOrCreateBridgeDM(ctx context.Context, cli *mautrix.Client, botID id.UserID) (id.RoomID, error) {
	joined, err := cli.JoinedRooms(ctx)
	if err != nil {
		return "", fmt.Errorf("list joined rooms: %w", err)
	}
	for _, roomID := range joined.JoinedRooms {
		members, err := cli.JoinedMembers(ctx, roomID)
		if err != nil {
			continue
		}
		if _, hasBot := members.Joined[botID]; hasBot && len(members.Joined) == 2 {
			return roomID, nil
		}
	}
	resp, err := cli.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Preset:   "trusted_private_chat",
		Invite:   []id.UserID{botID},
		IsDirect: true,
	})
	if err != nil {
		return "", fmt.Errorf("open DM with %s: %w", botID, err)
	}
	return resp.RoomID, nil
}

// runBridgeLogin sends "login" to the bridge bot and then relays: whatever
// the bot says is printed verbatim (no keyword matching — the bridge's own
// prompt wording, currently asking for an IG cookie, is free to change), and
// whatever the user types is sent straight back. It never inspects or
// persists what the user types beyond forwarding it into the room.
//
// The loop ends when the user enters a blank line or "/done" — not by
// parsing the bot's replies, so it keeps working if the bridge's own
// conversation shape changes — or when ctx is cancelled (Ctrl-C).
func runBridgeLogin(ctx context.Context, cli *mautrix.Client, roomID id.RoomID, botID id.UserID, stdout io.Writer, stdin io.Reader) error {
	if _, err := cli.SendText(ctx, roomID, "login"); err != nil {
		return fmt.Errorf("send login: %w", err)
	}
	fmt.Fprintln(stdout, "已經跟 bridge bot 說了 login，等它回覆…")
	fmt.Fprintln(stdout, "照著它問的貼上去就好；結束就打空白行或 /done，Ctrl-C 可以隨時中斷。")

	since := ""
	if baseline, err := cli.SyncRequest(ctx, 5000, since, "", false, ""); err == nil {
		// Only react to what happens after "login" was sent, not the room's
		// entire join/invite history.
		since = baseline.NextBatch
	}

	scanner := bufio.NewScanner(stdin)
	for {
		resp, err := cli.SyncRequest(ctx, 30000, since, "", false, "")
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("waiting for bridge bot: %w", err)
		}
		since = resp.NextBatch

		room, ok := resp.Rooms.Join[roomID]
		if !ok || len(room.Timeline.Events) == 0 {
			continue // long-poll timeout, nothing new yet
		}
		for _, evt := range room.Timeline.Events {
			if evt.Sender != botID || evt.Type != event.EventMessage {
				continue
			}
			if content := evt.Content.AsMessage(); content != nil {
				fmt.Fprintln(stdout, content.Body)
			}
		}

		if !scanner.Scan() {
			return fmt.Errorf("stdin closed before bridge login finished")
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "/done" {
			fmt.Fprintln(stdout, "結束等待 bridge 回覆；接下來 bridge 會自己把 IG 對話一個一個變成房間。")
			return nil
		}
		if _, err := cli.SendText(ctx, roomID, line); err != nil {
			return fmt.Errorf("send reply: %w", err)
		}
	}
}

// aliasSkeleton proposes [aliases] entries from every room the account has
// joined besides the bootstrap DM with the bridge bot itself, using whatever
// display name the puppet already carries. Best-effort: any failure here
// just leaves an entry (or the whole map) empty rather than failing setup.
func aliasSkeleton(ctx context.Context, cli *mautrix.Client, botID id.UserID) map[string]string {
	aliases := map[string]string{}
	joined, err := cli.JoinedRooms(ctx)
	if err != nil {
		return aliases
	}
	for _, roomID := range joined.JoinedRooms {
		members, err := cli.JoinedMembers(ctx, roomID)
		if err != nil {
			continue
		}
		for uid, member := range members.Joined {
			if uid == cli.UserID || uid == botID || member.DisplayName == "" {
				continue
			}
			aliases[string(uid)] = member.DisplayName
		}
	}
	return aliases
}
