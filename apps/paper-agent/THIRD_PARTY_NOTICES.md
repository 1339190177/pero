# Paper Agent notices

The Python research domain rules and public-literature adapters are derived
from Point Paper Agent and Scientific Viewer (Apache-2.0, Point contributors).
Reference source: `phodal/point`, revision
`dcd549a9cc8807eda5ce682eef4621afbd9a02d9`, specifically
`apps/paper-agent/{src,backend-src}` and
`apps/scientific-viewer/backend-src/literature.mjs`.
The Python implementation and Pero UI were written for this integration; no
Point UI components, generated bundles, execution runtime or private user data
are included. See [Apache-2.0](licenses/Apache-2.0.txt).

The independently bundled App UI includes React and ReactDOM 19.2.8 under MIT:
[React](licenses/React-LICENSE.txt), [ReactDOM](licenses/ReactDOM-LICENSE.txt),
and their [Scheduler](licenses/Scheduler-LICENSE.txt) dependency.
Their versions are pinned in the App's lockfile. The App SDK is supplied by this
repository at build time. No external research clone implementation is included.

Papers, author text, figures, research repositories and datasets retain their
own attribution and rights. Retrieval through a public API does not authorize
redistribution or scientific acceptance.
