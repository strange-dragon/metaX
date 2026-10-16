# Licensing and source reuse gate

## Observed licenses

- Amane ([sqzw-x/amane](https://github.com/sqzw-x/amane), snapshot `a2d45880e83df7047d461452965919692701e8c9)): repository `LICENSE` is GNU GPL version 3.
- MetaTube SDK ([metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go), snapshot `c62584675c593816cc07e7b5b977d14c7eb505a3`): repository `LICENSE` is Apache License 2.0.
- metaX itself: current `LICENSE` is Apache License 2.0.

## Consequence for migration

Do not copy Amane implementation files, test fixtures, captured responses, or substantial code-derived material into this Apache-2.0 repository until the project's licensing path is explicitly decided. The source and target have materially different licensing terms. A source file being publicly readable does not grant permission to relicense it under Apache-2.0.

This is a release blocker for a literal code port, not a reason to stop architectural analysis or compatibility testing.

## Available paths to resolve

1. **Obtain explicit permission or a suitable license grant from the relevant copyright holders** for the specific code to be ported and the intended distribution.
2. **Change metaX's licensing strategy** after the repository owner decides whether the project and its redistributed derivative code can be distributed under GPL-3.0-compatible terms. This requires a review of the full dependency, contribution, and distribution plan; do not make the change silently.
3. **Use a clean-room implementation only where legally appropriate**: write a behavior specification from observable public behavior and interface contracts, keep provenance and development notes, and avoid copying Amane source structure or expression. This is not an automatic legal safe harbor, especially if implementation knowledge is derived from the source; obtain legal advice if the boundary is unclear.

The MetaTube SDK being Apache-2.0 does not by itself settle whether GPL-derived provider code can be redistributed inside this Apache-licensed metaX project or downstream SDK artifacts. Do not give users a definitive compatibility conclusion without a qualified license review.

## Practical rule for this repository

Until this gate is resolved:

- Continue documenting capabilities, SDK interfaces, API compatibility, test requirements, and independently observable site behavior.
- Do not commit copied Amane code or its fixture corpus.
- Record the origin and license of every source file or fixture proposed for reuse.
- Review site-specific licenses and fixture provenance separately; do not assume the repository-level license answers every question.
