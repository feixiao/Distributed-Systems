package viewservice

import "time"

//
// This is a non-replicated view service for a simple
// primary/backup system.
//
// The view service goes through a sequence of numbered
// views, each with a primary and (if possible) a backup.
// A view consists of a view number and the host:port of
// the view's primary and backup p/b servers.
//
// The primary in a view is always either the primary
// or the backup of the previous view (in order to ensure
// that the p/b service's state is preserved).
//
// Each p/b server should send a Ping RPC once per PingInterval.
// The view server replies with a description of the current
// view. The Pings let the view server know that the p/b
// server is still alive; inform the p/b server of the current
// view; and inform the view server of the most recent view
// that the p/b server knows about.
//
// The view server proceeds to a new view when either it hasn't
// received a ping from the primary or backup for a while, or
// if there was no backup and a new server starts Pinging.
//
// The view server will not proceed to a new view until
// the primary from the current view acknowledges
// that it is operating in the current view. This helps
// ensure that there's at most one p/b primary operating at
// a time.
//

type View struct {
	Viewnum uint
	Primary string
	Backup  string
}

// clients should send a Ping RPC this often,
// to tell the viewservice that the client is alive.
const PingInterval = time.Millisecond * 100

// the viewserver will declare a client dead if it misses
// this many Ping RPCs in a row.
const DeadPings = 5

//
// Ping(): called by a primary/backup server to tell the
// view service it is alive, to indicate whether p/b server
// has seen the latest view, and for p/b server to learn
// the latest view.
//
// If Viewnum is zero, the caller is signalling that it is
// alive and could become backup if needed.
//

type PingArgs struct {
	Me      string // "host:port"
	Viewnum uint   // caller's notion of current view #
}

type PingReply struct {
	View View
}

//
// Get(): fetch the current view, without volunteering
// to be a server. mostly for clients of the p/b service,
// and for testing.
//

type GetArgs struct {
}

type GetReply struct {
	View View
}
