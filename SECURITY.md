# Security policy

Thank you for helping keep lagdoc and the people who use it safe.

## Reporting a vulnerability

Please report security problems **privately** through GitHub's [private vulnerability reporting](https://github.com/ag3ntsc4rn/lagdoc/security/advisories/new) (the **Report a vulnerability** button on the Security tab). Don't open a public issue, pull request or discussion.

Please include what you found, which version (`lagdoc --version`) and OS, how to reproduce it, and what an attacker could do with it. A minimal proof of concept helps.

## What to expect

lagdoc is maintained by one person in their spare time.

- **Acknowledgement within 7 days** of your report.
- An initial assessment, and a fix or a plan, as soon as practical after that. I'll keep you updated as it progresses.
- Fixes ship through the normal pull request process and a new release. Once a fix is released, I'll publish a GitHub security advisory where that makes sense.
- Please give me a reasonable chance to fix the problem before you disclose it publicly. I'm happy to agree a disclosure date with you.

## Scope

In scope:

- **The lagdoc binary**: for example, output that can be spoofed to mislead someone, a crafted process name or mount path that breaks the terminal (escape-sequence injection), or anything that reads or sends more than the README says.
- **Future `lagdoc fix` actions**: anything that changes or deletes more than it previewed, or acts without confirmation.
- **Releases and supply chain**: the GitHub Actions workflows, goreleaser config, published archives and checksums.

Out of scope:

- Problems that need an attacker who already has your user account or root on the machine.
- Vulnerabilities in dependencies without a way to reach them through lagdoc. Please report those upstream (Dependabot keeps lagdoc's dependencies current).
- Reports from automated scanners without a demonstrated impact.

## Safe harbour

If you make a good-faith effort to follow this policy, I consider your research authorised, won't pursue or support legal action against you, and will say so if anyone else does. Good faith means you test only on machines you own or are allowed to test, don't access or share other people's data, and report privately with a reasonable time to fix before disclosing.

## Recognition

lagdoc is free, so there's **no bug bounty**. With your permission, I'll credit you by name or handle in the release notes for the fix and in the security advisory.
