# idea: doctor flags store entries whose name contradicts their pack.yaml

**Date:** 2026-09-27
**From:** Opportunities note on sideshow#127 (aae-orc-oihza)

#127 stops new installs from storing a pack under a name its pack.yaml
contradicts. Stores filled before it may already hold one (the
aae-orc-cv1b6 repro left bmad content at packs/spectacle/6.12.0). A
doctor layer-1 check could compare each store directory's name with the
pack.yaml name inside it and report mismatches, without moving anything.

Not a commitment; no bd ticket.
