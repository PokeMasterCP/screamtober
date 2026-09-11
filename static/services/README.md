# Viewing-service assets

The predefined catalog is in `viewing_services.go`. Add a stable ID, display name,
and a bundled icon here to support a service. Set `Retired: true` to remove it from
new selections while preserving its display in historical challenges. Never reuse
an ID for another service. No schema change is needed to adjust the catalog.

Sources (downloaded September 11, 2026):

- Netflix, Plex, Apple TV, Paramount+, Tubi: [Simple Icons 15.0.0](https://github.com/simple-icons/simple-icons/tree/15.0.0/icons), CC0 (license included).
- Prime Video, Hulu, Disney+, Peacock: [dashboard-icons](https://github.com/homarr-labs/dashboard-icons/tree/ce550a844bad92ea19b5926cb887285c46bac01a/svg), Apache-2.0 (license included).
- Shudder: official [Shudder favicon](https://images.amcsvod.io/sh/favicon.png?w=196), linked by [shudder.com](https://www.shudder.com/).
- Theaters: original generic screen/seating icon for this project.

Brand marks belong to their respective owners. The icons identify the selected
viewing service and do not imply affiliation. Assets are served locally; no logo
requests are sent to third parties while browsing a challenge.
