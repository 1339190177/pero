"""Report and exact-quotation rules ported from Point Paper Agent (Apache-2.0)."""
from dataclasses import dataclass
import json
import re

from .model import REPORT_SCHEMA, Invalid, ResearchInput, canonical, mapping, sequence, text


@dataclass(frozen=True)
class EvidenceReference:
    paper_id: str
    quote: str

    @classmethod
    def parse_many(cls, value, cited_ids, papers):
        references = []
        for item in sequence(value, 8, "evidence"):
            item = mapping(item, "evidence reference")
            quote = item.get("quote")
            text(quote, 4000, "evidence quotation")
            ref = cls(text(item.get("paperId"), 64, "evidence source"), quote)
            if ref.paper_id not in cited_ids:
                raise Invalid("Evidence is outside the cited sources")
            ref.locate(papers)
            references.append(ref)
        return tuple(references)

    def locate(self, papers):
        paper = next((paper for paper in papers if paper.id == self.paper_id), None)
        if paper is None:
            raise Invalid("Evidence source is outside the frozen input")
        start = paper.content.find(self.quote)
        if start < 0:
            raise Invalid("Evidence quotation is absent from the frozen source")
        headings = re.findall(r"^#{1,6}\s+(.+)$", paper.content[:start], re.M)
        end = start + len(self.quote)
        return dict(paperId=paper.id, start=start, end=end, heading=headings[-1] if headings else "",
                    before=paper.content[max(0, start - 500):start], quote=self.quote, after=paper.content[end:end + 500])

    def to_dict(self):
        return dict(paperId=self.paper_id, quote=self.quote)


def paper_refs(value, allowed):
    values = sequence(value, 8, "paper IDs")
    if not values or any(not isinstance(item, str) or item not in allowed for item in values) or len(set(values)) != len(values):
        raise Invalid("Unknown or missing paper reference")
    return tuple(values)


@dataclass(frozen=True)
class ReportSection:
    heading: str
    content: str
    paper_ids: tuple[str, ...]
    evidence: tuple[EvidenceReference, ...]

    def to_dict(self):
        return dict(heading=self.heading, content=self.content, paperIds=list(self.paper_ids),
                    evidence=[ref.to_dict() for ref in self.evidence])


@dataclass(frozen=True)
class CodeFile:
    path: str
    language: str
    content: str

    @classmethod
    def parse(cls, value):
        value = mapping(value, "code file")
        path = text(value.get("path"), 300, "code path")
        if re.search(r"^[\\/]|^[a-z]:|[\x00-\x1f]", path, re.I) or ".." in re.split(r"[\\/]", path):
            raise Invalid("Code paths must be workspace-relative")
        content = value.get("content")
        text(content, 40000, "code")
        return cls(path, text(value.get("language"), 40, "language"), content)

    def to_dict(self):
        return dict(path=self.path, language=self.language, content=self.content)


@dataclass(frozen=True)
class Experiment:
    title: str
    paper_ids: tuple[str, ...]
    evidence: tuple[EvidenceReference, ...]
    status: str
    files: tuple[CodeFile, ...]
    commands: tuple[str, ...]
    expected: str
    observed: str
    limitations: str

    @classmethod
    def parse(cls, value, allowed, papers):
        value = mapping(value, "experiment")
        status = value.get("status")
        if status not in ("passed", "failed", "blocked", "not-run"):
            raise Invalid("Invalid reported experiment status")
        refs = paper_refs(value.get("paperIds"), allowed)
        evidence = EvidenceReference.parse_many(value.get("evidence", []), refs, papers)
        files = tuple(CodeFile.parse(file) for file in sequence(value.get("files"), 12, "code files"))
        commands = tuple(text(command, 2000, "command") for command in sequence(value.get("commands"), 12, "commands"))
        observed = text(value.get("observed"), 12000, "observed result")
        if status in ("passed", "failed") and (not files or not commands):
            raise Invalid("Reported execution needs code, commands and observations")
        return cls(text(value.get("title"), 200, "experiment title"), refs, evidence, status,
                   files, commands, text(value.get("expected"), 4000, "expected result"), observed,
                   text(value.get("limitations"), 4000, "experiment limitations"))

    def to_dict(self):
        return dict(title=self.title, paperIds=list(self.paper_ids), evidence=[ref.to_dict() for ref in self.evidence],
                    status=self.status, files=[file.to_dict() for file in self.files], commands=list(self.commands),
                    expected=self.expected, observed=self.observed, limitations=self.limitations, provenance="agent-reported")


@dataclass(frozen=True)
class ResearchReport:
    intent_id: str
    title: str
    sections: tuple[ReportSection, ...]
    experiments: tuple[Experiment, ...]
    limitations: str

    @classmethod
    def parse(cls, value, intent):
        value = mapping(value, "report")
        if value.get("schema") not in (REPORT_SCHEMA, "point.paper-agent-report.v1") or value.get("intentId") != intent.id:
            raise Invalid("Report does not match the frozen research input")
        allowed = {paper.id for paper in intent.papers}
        sections = []
        for section in sequence(value.get("sections"), 12, "sections"):
            section = mapping(section, "report section")
            refs = paper_refs(section.get("paperIds"), allowed)
            evidence = EvidenceReference.parse_many(section.get("evidence", []), refs, intent.papers)
            sections.append(ReportSection(text(section.get("heading"), 200, "heading"),
                                          text(section.get("content"), 24000, "content"), refs, evidence))
        if not sections:
            raise Invalid("A report needs at least one cited section")
        experiments = tuple(Experiment.parse(item, allowed, intent.papers) for item in sequence(value.get("experiments"), 6, "experiments"))
        if intent.verify_code and not experiments:
            raise Invalid("A code-verification request needs an experiment or an explicit blocked/not-run entry")
        title = text(value.get("title"), 200, "title")
        if len(title.encode("utf-8")) > 200:
            raise Invalid("Report title exceeds 200 UTF-8 bytes")
        result = cls(intent.id, title, tuple(sections), experiments, text(value.get("limitations"), 8000, "limitations"))
        if len(canonical(result.to_dict())) > 450000:
            raise Invalid("Report exceeds 450,000 bytes")
        return result

    @classmethod
    def from_text(cls, source, intent):
        if not isinstance(source, str) or len(source) > 500000:
            raise Invalid("Agent output exceeds the report limit")
        source = source.strip()
        match = re.fullmatch(r"```(?:json)?\s*\n([\s\S]*?)\n```", source)
        try:
            value = json.loads(match[1] if match else source)
        except (ValueError, RecursionError) as error:
            raise Invalid("No valid JSON report was returned; keep the output and correct its format") from error
        return cls.parse(value, intent)

    def to_dict(self):
        return dict(schema=REPORT_SCHEMA, intentId=self.intent_id, title=self.title,
                    sections=[section.to_dict() for section in self.sections],
                    experiments=[experiment.to_dict() for experiment in self.experiments],
                    limitations=self.limitations, acceptance="unverified")

    def markdown(self, intent: ResearchInput):
        # Reports are plain text; escape embedded HTML while retaining Markdown export.
        def prose(value):
            return value.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

        def sources(ids):
            papers = {paper.id: paper for paper in intent.papers}
            return " · ".join(f'[{prose(papers[item].title).replace("[", "(").replace("]", ")")}]({papers[item].url.replace(")", "%29")})' for item in ids)

        def quotations(refs):
            return "\n\n".join("> " + prose(ref.quote).replace("\n", "\n> ") + "\n>\n> " + sources([ref.paper_id]) for ref in refs)

        lines = [f"# {prose(self.title)}", "", "Acceptance: unverified · experiment results: agent-reported", "",
                 f"Frozen research input: `{self.intent_id}`", ""]
        for section in self.sections:
            lines.extend([f"## {prose(section.heading)}", "", prose(section.content), "", sources(section.paper_ids), "", quotations(section.evidence), ""])
        for experiment in self.experiments:
            lines.extend([f"## {prose(experiment.title)}", "", f"Agent-reported: {experiment.status}", "", sources(experiment.paper_ids), "",
                          f"Expected: {prose(experiment.expected)}", "", f"Observed: {prose(experiment.observed)}", "",
                          f"Limitations: {prose(experiment.limitations)}", "", quotations(experiment.evidence), ""])
            if experiment.commands:
                content = "\n".join(experiment.commands)
                fence = "`" * max(3, max((len(run) + 1 for run in re.findall(r"`+", content)), default=3))
                lines.extend(["Commands:", "", fence + "sh", content, fence, ""])
            for file in experiment.files:
                fence = "`" * max(3, max((len(run) + 1 for run in re.findall(r"`+", file.content)), default=3))
                language = re.sub(r"[^a-z0-9+-]", "", file.language, flags=re.I)
                lines.extend([f"### {prose(file.path)}", "", fence + language, file.content, fence, ""])
        lines.extend(["## Limitations", "", prose(self.limitations), "", "## Sources", ""])
        for paper in intent.papers:
            lines.append(f"- {sources([paper.id])} — {paper.content_kind}; {paper.content_digest}; {paper.fetched_at}")
        return "\n".join(lines)
