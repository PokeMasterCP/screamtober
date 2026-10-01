# Viewing-service assets

The predefined catalog is in [viewing_services.go](../../viewing_services.go). Add a stable ID, display name,
and a bundled icon here to support a service. Set `Retired: true` to remove it from
new selections while preserving its display in historical challenges. Never reuse
an ID for another service. No schema change is needed to adjust the catalog.

Icons are square, full-bleed tiles in the style of each service's app icon, named
after the service ID. Pages round the corners, so leave the artwork square. When
replacing an existing file, bump `serviceIconVersion` so browsers drop cached copies.

Sources (downloaded October 1, 2026), placed on a brand-colored square tile:

- Apple TV, Paramount+, Starz, Tubi: [Simple Icons 16.33.0](https://github.com/simple-icons/simple-icons/tree/16.33.0/icons), CC0 (license included).
- Netflix, Hulu, Peacock, Plex, YouTube: [dashboard-icons](https://github.com/homarr-labs/dashboard-icons/tree/f1d048d9885b97a7319e0a51e508217459f212df/svg), Apache-2.0 (license included).
- HBO Max: [HBO Max (2025).svg](https://commons.wikimedia.org/wiki/File:HBO_Max_(2025).svg), public domain (text logo).
- The Roku Channel: [The Roku Channel Logo.svg](https://commons.wikimedia.org/wiki/File:The_Roku_Channel_Logo.svg), public domain (text logo), stacked to fit a square.
- Official site icons, linked by each service's website: Amazon Prime Video
  ([primevideo.com](https://www.primevideo.com/) apple-touch icon), Disney+
  ([disneyplus.com](https://www.disneyplus.com/) apple-touch icon), Shudder
  ([shudder.com](https://www.shudder.com/) favicon), AMC+ ([amcplus.com](https://www.amcplus.com/)
  header logo), MGM+ ([mgmplus.com](https://www.mgmplus.com/) favicon), Pluto TV
  ([pluto.tv](https://pluto.tv/) apple-touch icon), and Fandango at Home
  ([athome.fandango.com](https://athome.fandango.com/) apple-touch icon).
- In theaters: original popcorn bucket icon for this project.

Brand marks belong to their respective owners. The icons identify the selected
viewing service and do not imply affiliation. Assets are served locally; no logo
requests are sent to third parties while browsing a challenge.
