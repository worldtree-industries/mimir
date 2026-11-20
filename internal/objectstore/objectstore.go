// Package objectstore holds Mimir's implementations of object storage, the
// [models.ObjectStore] mimir-server publishes Snapshots through and clients
// poll them from (ADR 0002, ADR 0006): [Memory] (tests) and [S3] (Ceph RGW and
// other S3-API stores).
//
// It is shared infrastructure rather than a Layer. The client Layer doesn't
// import it: clients are given a [models.ObjectReader], and read S3 by their
// own means.
package objectstore
