# idea: doctor warns when a pinned pack trails upstream

**Date:** 2026-09-27
**From:** Opportunities note on sideshow#128 (aae-orc-soh8q)

With pack.yaml 0.2.0 carrying a composition block, `sideshow list` says
a pack is pinned and as of when. It does not say how far behind that is
today. `sideshow-packs/scripts/pin-lag.sh` answers that from the same
data. A doctor layer could run the same comparison for installed packs
and warn, never fail (diagnostic, not gate).

Cost: network calls from doctor, per external module. Unreachable
upstreams must report "unknown", never "current" (the #35 review
finding).

Not a commitment; no bd ticket.
