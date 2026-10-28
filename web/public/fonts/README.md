# Font sources

Inter, IBM Plex Sans and JetBrains Mono are the bundled gpui-kit font subsets
from snapshot ec89cfc. Their license is in OFL.txt.

Workbench-regular.ttf and Workbench-bold.ttf are the original Workbench demo's
400/700 Chinese subsets converted from WOFF to TTF without changing outlines.
Their family metadata is normalized as Noto Sans CJK SC for consistent selection.

NotoSansSC-Regular.ttf and NotoSansSC-Bold.ttf are static weight-400/700 instances of Google's Noto Sans SC
variable font, retrieved from:
https://github.com/google/fonts/blob/main/ofl/notosanssc/NotoSansSC%5Bwght%5D.ttf
on 2026-10-28, under the SIL Open Font License. It was instantiated using
fontTools.varLib.instancer. Full-font family metadata is normalized to
Noto Sans SC Full for fallback registration. The Kit gallery subset identifies as
Thin and is intentionally not used for the workbench.
