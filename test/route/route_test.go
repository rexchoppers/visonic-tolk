package routetest

import (
	"reflect"
	"testing"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/route"
)

var (
	both    = route.State{MonitorConnected: true, VisonicConnected: true}
	noCloud = route.State{MonitorConnected: true}
	alone   = route.State{}
)

func frame(typ string, data []byte) powerlink31.Frame {
	return powerlink31.Frame{Type: typ, Data: data}
}

func b0() []byte   { return []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a} }
func e1() []byte   { return []byte{0x0d, 0xe1, 0x01, 0x02, 0x0a} }
func stop() []byte { return []byte{0x0d, 0x0b, 0xf4, 0x0a} }

func TestRoute(t *testing.T) {
	cases := []struct {
		name  string
		from  route.Peer
		frame powerlink31.Frame
		state route.State
		want  route.Plan
	}{
		{
			name: "panel message reaches both, and only visonic holds the gate",
			from: route.Panel, frame: frame(powerlink31.TypeBBA, b0()), state: both,
			want: route.Plan{Send: []route.Send{
				{To: route.Monitor},
				{To: route.Visonic, WantAck: true},
			}},
		},
		{
			name: "panel message with no cloud is acknowledged by tolk",
			from: route.Panel, frame: frame(powerlink31.TypeBBA, b0()), state: noCloud,
			want: route.Plan{Send: []route.Send{{To: route.Monitor}}, AckBack: true},
		},
		{
			name: "an adm-cid from the panel is not shown to home assistant",
			from: route.Panel, frame: frame(powerlink31.TypeAdmCID, b0()), state: both,
			want: route.Plan{Send: []route.Send{{To: route.Visonic, WantAck: true}}},
		},
		{
			name: "a panel ack goes to the cloud when it is connected",
			from: route.Panel, frame: frame(powerlink31.TypeAck, nil), state: both,
			want: route.Plan{Send: []route.Send{{To: route.Visonic}}},
		},
		{
			name: "a panel ack falls back to home assistant",
			from: route.Panel, frame: frame(powerlink31.TypeAck, nil), state: noCloud,
			want: route.Plan{Send: []route.Send{{To: route.Monitor}}},
		},
		{
			name: "an adm ack is never shown to home assistant",
			from: route.Panel, frame: frame(powerlink31.TypeAdmAck, nil), state: noCloud,
			want: route.Plan{},
		},
		{
			name: "a panel ack with nobody listening goes nowhere",
			from: route.Panel, frame: frame(powerlink31.TypeAck, nil), state: alone,
			want: route.Plan{},
		},
		{
			name: "the cloud reaches the panel",
			from: route.Visonic, frame: frame(powerlink31.TypeBBA, b0()), state: both,
			want: route.Plan{Send: []route.Send{{To: route.Panel, WantAck: true}}},
		},
		{
			name: "a cloud disconnect is answered and the link dropped",
			from: route.Visonic, frame: frame(powerlink31.TypeBBA, message.Disconnect), state: both,
			want: route.Plan{AckBack: true, Drop: true},
		},
		{
			name: "home assistant reaches the panel",
			from: route.Monitor, frame: frame(powerlink31.TypeBBA, b0()), state: both,
			want: route.Plan{Send: []route.Send{{To: route.Panel, WantAck: true}}},
		},
		{
			name: "an action from home assistant is answered here, not forwarded",
			from: route.Monitor, frame: frame(powerlink31.TypeBBA, e1()), state: both,
			want: route.Plan{Local: true, AckBack: true},
		},
		{
			name: "a stop from home assistant is acknowledged but not passed on",
			from: route.Monitor, frame: frame(powerlink31.TypeBBA, stop()), state: both,
			want: route.Plan{AckBack: true},
		},
		{
			name: "a b0 from home assistant is not filtered",
			from: route.Monitor, frame: frame(powerlink31.TypeBBA, b0()), state: both,
			want: route.Plan{Send: []route.Send{{To: route.Panel, WantAck: true}}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := route.Route(c.from, c.frame, c.state); !reflect.DeepEqual(got, c.want) {
				t.Errorf("\n got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestNakIsTreatedAsAnAck(t *testing.T) {
	got := route.Route(route.Panel, frame(powerlink31.TypeNak, nil), both)

	want := route.Plan{Send: []route.Send{{To: route.Visonic}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAnUnknownPeerRoutesNowhere(t *testing.T) {
	if got := route.Route("nobody", frame(powerlink31.TypeBBA, b0()), both); len(got.Send) != 0 {
		t.Errorf("got %+v, want nothing sent", got)
	}
}
