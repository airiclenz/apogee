---
title: "Before writing outside this repository: global config, system packages and other checkouts are the user's"
labels: [seed:baseline, seed-text:0.17.1:cd2c1fbd22fe]
---

Keep changes inside this repository. Ask the user before changing anything outside it:
`git config --global`, shell profiles, system or global package installs (`sudo apt`,
`npm i -g`, `pip install` outside a virtual environment), other checkouts, and configuration
files in the home directory. A temporary directory of your own is fine. On a machine that
exists only for this session (a CI job, a disposable container) installs are fine.
