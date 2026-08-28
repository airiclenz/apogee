---
name: d365-upgrade-audit
description: Runs an AI code audit for Dynamics CRM / Dynamics 365 on-premises (e.g. 8.2) to Power Platform / Dataverse Online upgrades. Produces a persistent, mergeable inventory of required work — .NET Framework → modern .NET, CrmServiceClient/SOAP → Dataverse ServiceClient, GAC references, plugin sandbox compatibility, Xrm.Page-era jScript, online behavioral limits (pagination, service protection), deprecated customizations (dialogs, legacy web client) — plus a dependency map and suggested order of operations. Re-runnable on a repo, project, file, or unzipped solution folder; results merge into one master report whose findings, ordering and effort sums feed project planning and estimation. Uses sub-agents for crawling so orchestrator context stays small. Use whenever the user mentions auditing, assessing, or inventorying Dynamics/CRM/Dataverse code for an online migration or upgrade, checking plugins/jScript/customizations for online compatibility, or asks "what work is needed" to move CRM code to the cloud.
argument-hint: "<audit root> (optionally: scope <project|path>; solution folder <path>; context: <notes>)"
---

Fixture body — see testdata/library/README.md.
