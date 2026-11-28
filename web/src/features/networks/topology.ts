// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { Network } from "../../api/queries";

/** Colors cycled over networks; edges and the network card share one. */
const COLORS = ["blue", "teal", "grape", "orange", "cyan", "pink", "lime", "indigo"];

type Link = { network: string; color: string; ips: string[] };

export type TopologyContainer = {
  id: string;
  name?: string;
  running: boolean;
  links: Link[];
};

/** Container name, or the short ID of an unnamed one. */ export const label = (c: {
  id: string;
  name?: string;
}) => c.name || c.id.slice(0, 12);

export const color = (i: number) => COLORS[i % COLORS.length] ?? "blue";

/**
 * Groups the containers of all networks: a container attached to several networks
 * appears once with one address line per network. Containers are ordered by their
 * first network so that edges cross as little as possible.
 */
export function topologyContainers(networks: Network[]): TopologyContainer[] {
  const byId = new Map<string, TopologyContainer>();
  const first = new Map<string, number>();
  networks.forEach((n, i) => {
    for (const c of n.containers) {
      let node = byId.get(c.id);
      if (!node) {
        node = { id: c.id, name: c.name, running: c.running, links: [] };
        byId.set(c.id, node);
        first.set(c.id, i);
      }
      node.running ||= c.running;
      node.links.push({ network: n.name, color: color(i), ips: c.ips ?? [] });
    }
  });
  return [...byId.values()].sort(
    (a, b) => (first.get(a.id) ?? 0) - (first.get(b.id) ?? 0) || label(a).localeCompare(label(b)),
  );
}
