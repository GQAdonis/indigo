package rules

import (
	"context"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"

	"github.com/stretchr/testify/assert"
)

func TestBadHashtagPostRule(t *testing.T) {
	ctx := context.Background()
	assert := assert.New(t)

	engine := automod.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{
		Text: "some post blah",
	}
	evt1 := engine.NewRecordEvent(am1, path, cid1, &p1)
	assert.NoError(BadHashtagsPostRule(ctx, &evt1, &p1))
	assert.Empty(evt1.RecordFlags)

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	evt2 := engine.NewRecordEvent(am1, path, cid1, &p2)
	assert.NoError(BadHashtagsPostRule(ctx, &evt2, &p2))
	assert.NotEmpty(evt2.RecordFlags)
}
