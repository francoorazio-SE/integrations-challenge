# DECISIONS
For the evaluation process, please do not consider the kredexp importer. I just wanted to complete at least a part of the request to see it working, but it was all done after the 6 hours cap, so it should not be considered.

## 1. Channel chosen, and what I gave up

REST ingest for master data: reuses the same Go structs already deserialized from the ERP, no
manifest files, staging directories or sha256 hashing to write. Gave up the file channel's
structural guarantee: `record_count` and `sha256` verified before a single record is applied. I'd
reconsider at very high master-data volumes, where that all-or-nothing guard matters more than
getting per-record results back immediately.

## 2. Format chosen, and what I gave up

JSON. None of `supplier`, `purchase_order` or `purchase_order_line` need an embedded list, so
nothing is lost in expressiveness. Cost: JSON repeats field names per record, so fewer records fit
under the 1'000-record  cap than a flatter format would. At 100x the volume I'd pick
CSV, for the smaller per-record footprint and fewer number of records.

## 3. Ambiguities I found, the reading I chose, and the evidence

`CHALLENGE.md`'s phase 1 says to deliver UoM conversions to the twin ("Phase 1, master data. Read suppliers, purchase orders, purchase order lines and UoM conversions from the ERP's paginated REST surface "). The "curl.exe -X POST -H 'X-Admin-Token: miniblp-admin-token' -d "{\`"scenario\`":\`"S0\`",\`"seed\`":20260416}" http://127.0.0.1:8081/admin/v1/seed" itself states that uom_conversion has already been uploaded by another integration. Its Body it self contains the number od UoM {"scenario":"S0","seed":20260416,"cost_centers":60,"uom_conversions":15,"state_digest":"b1f372c142a7f5c2f0d39d249be542405db74fde725dfe5274cb2c32dcb7bca2","source_system":"other-integration"}
According to the documentation  file's name format for KRED files is KRED_<Mandant>_<JJJJMMTT>_<Lauf> where Lauf is a 3 digit integer starting from 1 every day. The run frequency is not declared, if the file is delivered every 10 minutes, in 1 day it could receive over 100 files, that might overrride existing file and skipping invoices if the counter restart or does not change after 999.

## 4. What I would ask the customer

The Head of Shared Services asked to post everything and clean up in SAP afterwards
(`CSM-ticket-4412`), which directly conflicts with SLA rule 1, nothing unmatched is ever posted.
The SLA is explicit that the rule wins and the conflict gets escalated, so I would raise this with
her directly. In the meantime, unmatched invoices would still become exceptions, never postings to avoid any possible incorecct invoice posting. I would also ask about frequency of the creation of KRED files.

## 5. Where I stopped

Phase 1 (master data) is implemented: cold load, delta load from a persisted watermark, and REST
ingest with chunking. Not verified end to end against a live twin.

Phases 2 (legacy delivery), 3 (FX) and 4 (posting, reports) are not started. The `kredexp-2.1` Go importer is still the provided stub. Running checks today fails every scenario: no report files are written.

## 6. What I would do next

1. Write `run.json` / `postings.csv` / `exceptions.csv`, even empty, and the correct exit code:
   nothing downstream is measurable without them.
2. Phase 2's file relay, to unlock the twin's matching against the master data already loaded.
3. The `kredexp-2.1` parser, scored independently and carrying the exercise's strongest signal. 
4. Phases 3 and 4, to close the loop.

---

This is my first project in Go. My background is AL (Microsoft Dynamics 365 Business Central),
where the language, the tooling and the idioms are very different, so a meaningful part of the
time went into learning Go itself rather than into the business logic above. I used Claude Code
throughout, as `CHALLENGE.md` invites, and did my best within that constraint. I did not want to deliver a full package wrote by an AI assistant that I could not explain, I preferred to focus on a small part of the project, understand it and be sure it relfected the request rather then provide something that might have worked but with no clue of how it works. I'm aware the result is insufficient, I couln't event test it correctly because I ran out of time and working on a Windows machine I would need to install WSL2 or create a Linux container but I prefer to be honest and stop working on it after reaching the time cap required by the challenge.
