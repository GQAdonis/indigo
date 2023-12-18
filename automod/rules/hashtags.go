package rules

import (
	"context"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var (
	_ automod.PostRuleFunc = BadHashtagsPostRule
	_ automod.PostRuleFunc = TooManyHashtagsPostRule
)

// looks for specific hashtags from known lists
func BadHashtagsPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	for _, tag := range ExtractHashtags(post) {
		tag = NormalizeHashtag(tag)
		if evt.InSet("bad-hashtags", tag) {
			evt.AddRecordFlag("bad-hashtag")
			break
		}
	}
	return nil
}

// if a post is "almost all" hashtags, it might be a form of search spam
func TooManyHashtagsPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	tags := ExtractHashtags(post)
	tagChars := 0
	for _, tag := range tags {
		tagChars += len(tag)
	}
	tagTextRatio := float64(tagChars) / float64(len(post.Text))
	// if there is an image, allow some more tags
	if len(tags) > 4 && tagTextRatio > 0.6 && post.Embed.EmbedImages == nil {
		evt.AddRecordFlag("many-hashtags")
	} else if len(tags) > 7 && tagTextRatio > 0.8 {
		evt.AddRecordFlag("many-hashtags")
	}
	return nil
}
