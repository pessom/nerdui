// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
)

// AllNamespaces as the namespace query parameter of a list asks for every namespace (#276),
// as the event stream already accepts.
const AllNamespaces = "*"

// listNamespaces lists items of the request's namespace, or of every namespace when it is
// AllNamespaces. tag stores the namespace in each item. It returns the namespace to report.
func listNamespaces[T any](r *http.Request, list func(ctx context.Context, ns string) ([]T, error), tag func(*T, string)) (string, []T, error) {
	ctx := r.Context()
	if r.URL.Query().Get("namespace") != AllNamespaces {
		ns, err := Namespace(r)
		if err != nil {
			return "", nil, err
		}
		items, err := list(ctx, ns)
		for i := range items {
			tag(&items[i], ns)
		}
		return ns, items, err
	}
	nss, err := EnvFrom(ctx).Runtime.System().Namespaces(ctx)
	if err != nil {
		return "", nil, err
	}
	all := []T{}
	for _, ns := range nss {
		items, err := list(ctx, ns)
		if err != nil {
			return "", nil, fmt.Errorf("namespace %s: %w", ns, err)
		}
		for i := range items {
			tag(&items[i], ns)
		}
		all = append(all, items...)
	}
	return AllNamespaces, all, nil
}
