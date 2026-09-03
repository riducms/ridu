# Security policy

## Reporting a vulnerability

Please report suspected vulnerabilities through
[GitHub's private vulnerability reporting form](https://github.com/riducms/ridu/security/advisories/new).
Do not open a public issue, discussion, or pull request containing exploit details, secrets, or
unredacted production data.

Include the affected Ridu version, deployment shape, reproduction steps, impact, and any proposed
mitigation. If the report needs files that should not be uploaded to GitHub, say so in the private
report and a maintainer will arrange a secure transfer.

Maintainers aim to acknowledge a report within three business days and provide an initial severity
assessment within seven business days. Updates will follow at least every seven days while a fix is
in progress. Please allow coordinated remediation and release time before public disclosure.

## Supported versions

Ridu is pre-1.0 software. Only the latest released minor line receives security fixes. A fix may
remove an unsafe API without a compatibility shim when retaining it would preserve the
vulnerability. The precise compatibility policy is documented in the
[public release guide](../website/src/content/docs/releases.md).

Security support covers the framework, CLI, official npm packages, official plugins, generated
contracts, and embedded admin. Application configuration, custom plugins, infrastructure, and
third-party storage providers remain the application owner's responsibility, though reports that
show an unsafe framework default are welcome.

## Disclosure and credit

Ridu will coordinate a security advisory, fixed release, and credit with the reporter unless they
prefer anonymity. Please avoid testing against systems or data you do not own, persistence beyond
what is needed to demonstrate impact, denial of service, and social engineering.
