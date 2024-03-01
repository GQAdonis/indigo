package search

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestParseQuery(t *testing.T) {
	ctx := context.Background()
	assert := assert.New(t)
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		Handle: syntax.Handle("known.example.com"),
		DID:    syntax.DID("did:plc:abc222"),
	})

	var q string
	var f []map[string]interface{}

	q, f = ParseQuery(ctx, &dir, "")
	assert.Equal("", q)
	assert.Empty(f)

	p1 := "some +test \"with phrase\" -ok"
	q, f = ParseQuery(ctx, &dir, p1)
	assert.Equal(p1, q)
	assert.Empty(f)

	p2 := "missing from:missing.example.com"
	q, f = ParseQuery(ctx, &dir, p2)
	assert.Equal("missing", q)
	assert.Empty(f)

	p3 := "known from:known.example.com"
	q, f = ParseQuery(ctx, &dir, p3)
	assert.Equal("known", q)
	assert.Equal(1, len(f))

	p4 := "from:known.example.com"
	q, f = ParseQuery(ctx, &dir, p4)
	assert.Equal("*", q)
	assert.Equal(1, len(f))

	p5 := `from:known.example.com "multi word phrase" coolio blorg`
	q, f = ParseQuery(ctx, &dir, p5)
	assert.Equal(`"multi word phrase" coolio blorg`, q)
	assert.Equal(1, len(f))

	p6 := `from:known.example.com #cool_tag some other stuff`
	q, f = ParseQuery(ctx, &dir, p6)
	assert.Equal(`some other stuff`, q)
	assert.Equal(2, len(f))

	p7 := `https://bsky.app/profile/did:plc:q6gjnaw2blty4crticxkmujt/post/3kmmi33vc2l2w`
	q, f = ParseQuery(ctx, &dir, p7)
	assert.Equal(`*`, q)
	assert.Equal(1, len(f))
	assert.Equal([]map[string]interface{}{
		{"term": map[string]interface{}{"embed_aturi": "at://did:plc:q6gjnaw2blty4crticxkmujt/app.bsky.feed.post/3kmmi33vc2l2w"}},
	}, f)

	p8 := `https://bsky.app/profile/known.example.com/post/3kmmi33vc2l2w`
	q, f = ParseQuery(ctx, &dir, p8)
	assert.Equal(`*`, q)
	assert.Equal(1, len(f))
	assert.Equal([]map[string]interface{}{
		{"term": map[string]interface{}{"embed_aturi": "at://did:plc:abc222/app.bsky.feed.post/3kmmi33vc2l2w"}},
	}, f)
}
