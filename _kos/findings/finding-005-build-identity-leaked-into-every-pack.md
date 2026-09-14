# finding-005: the frozen-composition pipeline shipped its own build identity into every bmad pack

**Date:** 2026-09-13
**Subject:** build-bmad.sh writing CI build-environment answers into shipped pack content
**Occasion:** the 988gh + m79qn re-issue fix (sideshow-packs PR #34)
**Evidence:** the two bd reports (aae-orc-988gh, aae-orc-m79qn); the 6.10.0 store
pack on disk; a probe of `scripts/resolve_config.py`; CI test-build run
`34725812548` (green) and its downloaded artifact; the earlier failing run
`34725681166`. sideshow-packs commits `596be04` and `3f1af92`.

## Why this is worth writing down

finding-004 named the class from one direction: every gate compared the captured
tree to the tag it came from, which proves transport and says nothing about the
tree. This is the same class from a second direction. The pipeline runs the
upstream installer non-interactively, so it must answer the installer's prompts,
and it answers them from the machine doing the build. Two of those answers are
identity, and the installer bakes them into config that ships and that the pack's
own resolver serves inside real consumer repositories.

The organizing observation, the finding under the finding:

> A frozen-composition pack should assert facts about the consumer's world, or
> none, never facts about the machine that assembled it. Transport fidelity does
> not catch this, because the leaked values are faithfully captured. They are
> just answers to the wrong question.

## What leaked, and where

Two installer answers, both build-environment state:

1. `user_name = "arcaven-ci"` (the CI sentinel passed as `--user-name`). It lands
   in `config.user.toml` and in every module `config.yaml` (`core`, `bmm`, `cis`,
   `gds`, `tea`, `bmb`, `wds`). 197 skill files read `user_name`, so it is live
   content, not an inert record. It has shipped in every published bmad pack since
   6.10.0. (aae-orc-988gh)
2. `project_name = "install"` (the basename of the pipeline's working directory,
   which the installer records from `--directory`). It lands in `config.toml` and
   `core/config.yaml`. `resolve_config.py` serves it, so nine skills saw a project
   named `install` regardless of where they ran. It has shipped since 6.11.0,
   where the field is new upstream. (aae-orc-m79qn)

Renaming the working directory only swaps one wrong value for another, because
`project_name` is meant to name the consuming project and the pipeline has no
consuming project. That ruled out the rename and pointed at omission, which
raised the one open question below.

## The probe: resolve_config.py tolerates an absent key

`resolve_config.py` is a four-layer TOML merge: `config.toml` (required) then
`config.user.toml` then `custom/config.toml` then `custom/config.user.toml`.
The file is required; individual keys are not. Tested against the 6.10.0
resolver with a synthetic config:

- An absent dotted key returns the internal `_MISSING` sentinel and is omitted
  from the merged output. The resolver exits 0. No crash, no fallback value.
- Scalar merge is override-wins, so a value set in the consumer's
  `custom/config.user.toml` supersedes a shipped value and equally fills an
  absent one.

So omission is safe and reversible: a skill reading `core.user_name` or
`core.project_name` gets a missing key and its own fallback, never a wrong value,
and a consumer sets a real identity in the custom layer that the sideshow bridge
already presents. This is why the fix omits the keys rather than substituting a
placeholder token; a token would need install-time injection that does not exist
yet (aae-orc-jy1j), and an uninjected token ships worse than an absent key.

## The fix

`scripts/neutralize-ci-identity.py`, run from `build-bmad.sh` on the staged tree
before the file manifest, removes the `user_name` and `project_name` assignment
lines from `config.toml`, `config.user.toml`, and the module `config.yaml` files.

Two properties earn their place:

- **Line-based removal, not a TOML/YAML round-trip.** A parser would reformat the
  file and produce a large diff against a native install. The pack's value is that
  its divergence from a native install is small and auditable, so only the identity
  lines are dropped and every comment, blank line, and other key stays byte-for-byte
  as the installer wrote it. Verified: a skill that references `user_name` in prose
  is untouched, because the scrub matches assignment lines, not the key as a word.
- **Fail-closed.** After scrubbing, any residual sentinel anywhere in the tree
  aborts the build. The scrub logic being correct is not enough on its own; the
  gate is what stops a future build from silently reintroducing the leak.

`install.meta` records the neutralization in a `post_install` block
(`schema_version` 0.1.2 to 0.1.3, additive), so the provenance stays complete
rather than hiding that the build passed a sentinel.

## What the CI build caught that the unit test could not

The scrub had passing unit tests against a synthetic leaked tree while the first
real CI build failed. The neutralize call resolved its script path from
`${BASH_SOURCE[0]}` after step 1 had `cd`ed into the isolated install directory,
so the path expanded to a directory that no longer existed and the build aborted.
The unit test could not see it, because it invoked the scrub directly rather than
through the pipeline. The fix captures `SCRIPT_DIR` once before any `cd`.

The decisive evidence that the leak was gone was not the green test. It was
extracting the downloaded artifact and counting: `grep -c arcaven-ci` over the
whole tarball returned 0, no identity assignment survived, and `file_count`
stayed 2023, unchanged from the base 6.12.0 build. A passing test reports that
code behaved; the artifact grep reports what shipped.

## Scope, and what remains open

The fix is in the pipeline, so it is version-agnostic; on 6.10.0, which has no
`project_name`, the scrub finds nothing to remove and the fail-closed gate still
runs. The re-issue is per-version and a separate deliberate act: the published
6.12.0 pack stays leaky until `bmad-v6.12.0-r2` is tagged and published (runbook
step 5). Both bd tickets stay open until then. Older published packs carry the
same leak and can re-issue through the same pipeline.

These are pipeline defects, not upstream wrinkles, so nothing was added to the
pack-support register; the register tracks upstream version shifts, and this
shift was ours.

One watch-thread. At 6.12.0 the installer also writes
`communication_language = "English"` under `[core]`. That is a build-environment
answer too, but it is a neutral locale default and not identity, so it was left
in place. It flags the general question: other installer answers derived from the
build environment may warrant the same review, and the neutralize step is the
place to extend if one does.

## The generalizable rule

Two, and the second is finding-004's rule reached from the output side:

1. A packaging pipeline that answers an installer non-interactively injects
   build-environment state into the artifact. Any such answer that describes the
   builder rather than the consumer is a leak, and transport-fidelity gates cannot
   see it because the value is captured faithfully. Neutralize build-environment
   answers in the staged tree before packaging, and gate the neutralization
   fail-closed.
2. A green test is evidence that code behaved, not that the artifact is correct.
   The check that decides whether a defect shipped reads the shipped artifact.

Node candidate for ratification (not filed here, per the ratification gate): a
bedrock element stating that a pack asserts facts about the consumer's world or
none, never about the machine that built it. Left for the operator or the harvest
owner to deliberate rather than created unilaterally.

## Cross-references

- finding-004 (the same class from the transport-fidelity side)
- sideshow-packs PR #34; `scripts/neutralize-ci-identity.py`;
  `scripts/build-bmad.sh`; `docs/release-notes/bmad-v6.12.0-r2.md`
- `registry/pack-support-revalidation-runbook.md` (the -rN re-issue is step 5)
- bd: aae-orc-988gh, aae-orc-m79qn (open); aae-orc-jy1j (install-time identity
  injection, the token path this fix deliberately did not take)
- orc findings on the same theme: finding-108 (-rN re-issue channel), finding-147
  (an unoverridable CI default decided artifact contents), finding-094 (unshaping
  over plugin delivery), finding-157 (verification that answers without checking)
