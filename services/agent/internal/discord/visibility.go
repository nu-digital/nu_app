package discord

import (
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// チャンネルの公開・非公開（@everyoneが閲覧できるか）。
// ぬるはBotの管理者権限で全チャンネルを読むが、**@everyoneが見られないチャンネルの内容は、検索・要約・日誌・週報・内省の材料にしない**
// （運営だけのチャンネルの内容が、権限のない人や公開の場に出ないように）。記録にはis_publicを付けて区別する。

// Overwrite はチャンネルの権限の上書き（ロール分のみ使う）。
type Overwrite struct {
	ID     string
	IsRole bool
	Allow  int64
	Deny   int64
}

// ViewableByEveryone は、@everyone（ロールID＝サーバーID）がチャンネルを見られるか。
// base は@everyoneロールのサーバー全体の権限。上書きは「拒否→許可」の順に適用する。管理者権限を持つなら見られる。
func ViewableByEveryone(base int64, ows []Overwrite, everyoneID string) bool {
	perms := base
	if perms&discordgo.PermissionAdministrator != 0 {
		return true
	}
	for _, o := range ows {
		if o.IsRole && o.ID == everyoneID {
			perms &^= o.Deny
			perms |= o.Allow
		}
	}
	return perms&discordgo.PermissionViewChannel != 0
}

func overwritesOf(ch *discordgo.Channel) []Overwrite {
	var out []Overwrite
	for _, o := range ch.PermissionOverwrites {
		out = append(out, Overwrite{ID: o.ID, IsRole: o.Type == discordgo.PermissionOverwriteTypeRole, Allow: o.Allow, Deny: o.Deny})
	}
	return out
}

type visCache struct {
	mu sync.Mutex
	m  map[string]visEntry
}

type visEntry struct {
	public bool
	at     time.Time
}

const visTTL = 5 * time.Minute

func (c *visCache) get(id string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Since(e.at) > visTTL {
		return false, false
	}
	return e.public, true
}

func (c *visCache) put(id string, public bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]visEntry{}
	}
	c.m[id] = visEntry{public: public, at: time.Now()}
}

// everyoneBase は@everyoneロールの権限（サーバーのロールから）。取れないときは ok=false。
func (g *Gateway) everyoneBase() (int64, bool) {
	if gd, err := g.s.State.Guild(g.guildID); err == nil {
		for _, r := range gd.Roles {
			if r.ID == g.guildID {
				return r.Permissions, true
			}
		}
	}
	roles, err := g.s.GuildRoles(g.guildID)
	if err != nil {
		return 0, false
	}
	for _, r := range roles {
		if r.ID == g.guildID {
			return r.Permissions, true
		}
	}
	return 0, false
}

// channelPublic は、チャンネル（スレッドなら親）を@everyoneが見られるか。**判定できないときは非公開**（安全側）。
// 非公開スレッドは非公開。
func (g *Gateway) channelPublic(channelID string) bool {
	if v, ok := g.vis.get(channelID); ok {
		return v
	}
	pub := g.computePublic(channelID, 0)
	g.vis.put(channelID, pub)
	return pub
}

func (g *Gateway) computePublic(channelID string, depth int) bool {
	if depth > 2 {
		return false
	}
	ch, err := g.s.State.Channel(channelID)
	if err != nil {
		if ch, err = g.s.Channel(channelID); err != nil {
			return false
		}
	}
	if ch.Type == discordgo.ChannelTypeGuildPrivateThread {
		return false
	}
	if ch.IsThread() {
		if ch.ParentID == "" {
			return false
		}
		return g.computePublic(ch.ParentID, depth+1)
	}
	base, ok := g.everyoneBase()
	if !ok {
		return false
	}
	return ViewableByEveryone(base, overwritesOf(ch), g.guildID)
}
