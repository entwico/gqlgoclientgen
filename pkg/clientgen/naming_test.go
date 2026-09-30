package clientgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"id", "ID"},
		{"ID", "ID"},
		{"url", "URL"},
		{"api", "API"},
		{"userId", "UserID"},
		{"userID", "UserID"},
		{"user_id", "UserID"},
		{"api_key", "APIKey"},
		{"httpServer", "HTTPServer"},
		{"HTTPServer", "HTTPServer"},
		// unknown acronyms inside mixed-case names are preserved
		{"streamHLS", "StreamHLS"},
		{"posterJPG", "PosterJPG"},
		// words that merely contain an initialism must not be mangled
		{"identifier", "Identifier"},
		{"paid", "Paid"},
		{"hybrid", "Hybrid"},
		{"candidate", "Candidate"},
		{"urlaub", "Urlaub"},
		// SCREAMING_SNAKE_CASE enum values
		{"FOO_BAR", "FooBar"},
		{"HTTP_ERROR", "HTTPError"},
		{"SELFHOSTED", "Selfhosted"},
		// mixed
		{"listPosts", "ListPosts"},
		{"list_posts", "ListPosts"},
		{"metaTitle", "MetaTitle"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, toPascalCase(tt.in))
		})
	}
}

func TestToLowerCamel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"id", "id"},
		{"ID", "id"},
		{"post_id", "postID"},
		{"postId", "postID"},
		{"statusFilter", "statusFilter"},
		{"status_filter", "statusFilter"},
		{"URL", "url"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, toLowerCamel(tt.in))
		})
	}
}

func TestFieldNameToGoName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"id", "ID"},
		{"title", "Title"},
		{"user_id", "UserID"},
		// leading underscores move to the end so the name stays exported
		{"__typename", "Typename__"},
		{"_private", "Private_"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, fieldNameToGoName(tt.in))
		})
	}
}

func TestEnumValueToConstName(t *testing.T) {
	assert.Equal(t, "ColorFooBar", enumValueToConstName("Color", "FOO_BAR"))
	assert.Equal(t, "StatusHTTPError", enumValueToConstName("Status", "HTTP_ERROR"))
	assert.Equal(t, "VideoProviderYoutube", enumValueToConstName("VideoProvider", "YOUTUBE"))
}
