package creativecanvas

import (
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
)

func TestFitMediaSizeKeepsAspectOnUntouchedNodesOnly(t *testing.T) {
	t.Parallel()
	ptr := func(v int) *int { return &v }
	portrait := creativecontent.Revision{Kind: "image", Media: []creativecontent.MediaObject{{Role: "display", Width: ptr(400), Height: ptr(600)}, {Role: "original", Width: ptr(1000), Height: ptr(1500)}}}
	cases := []struct {
		name string
		node Node
		rev  creativecontent.Revision
		want float64
	}{
		{"portrait image on default node", Node{Width: 280, Height: 180}, portrait, 494},
		{"wide image clamps to minimum", Node{Width: 280, Height: 180}, creativecontent.Revision{Kind: "image", Media: []creativecontent.MediaObject{{Role: "original", Width: ptr(4000), Height: ptr(100)}}}, 140},
		{"tall image clamps to maximum", Node{Width: 280, Height: 180}, creativecontent.Revision{Kind: "video", Media: []creativecontent.MediaObject{{Role: "original", Width: ptr(100), Height: ptr(4000)}}}, 640},
		{"resized node is left alone", Node{Width: 320, Height: 180}, portrait, 180},
		{"audio keeps the default box", Node{Width: 280, Height: 180}, creativecontent.Revision{Kind: "audio", Media: []creativecontent.MediaObject{{Role: "original"}}}, 180},
		{"missing dimensions keep the default box", Node{Width: 280, Height: 180}, creativecontent.Revision{Kind: "image", Media: []creativecontent.MediaObject{{Role: "original"}}}, 180},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			n := c.node
			fitMediaSize(&n, c.rev)
			if n.Height != c.want || n.Width != c.node.Width {
				t.Fatalf("got %vx%v, want %vx%v", n.Width, n.Height, c.node.Width, c.want)
			}
		})
	}
}
