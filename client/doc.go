// Package client is the layer that Deployables use to read their Configs.
//
// client may import models. It must not import api, service, data or any
// internal package, so that other Projects can use everything it exposes: its
// options take models types, such as [models.ObjectReader] and
// [models.Clock].
package client
