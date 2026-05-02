<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>agent-brain</id>
    <version>__VERSION__</version>
    <packageSourceUrl>https://github.com/NanoCycles/agent-brain</packageSourceUrl>
    <owners>NanoCycles</owners>
    <title>agent-brain</title>
    <authors>NanoCycles</authors>
    <projectUrl>https://github.com/NanoCycles/agent-brain</projectUrl>
    <licenseUrl>https://github.com/NanoCycles/agent-brain/blob/main/LICENSE</licenseUrl>
    <requireLicenseAcceptance>false</requireLicenseAcceptance>
    <tags>agent-brain ai agent cli mcp codex cursor claude developer-tool golang neo4j sqlite</tags>
    <summary>Local context intelligence CLI for AI coding agents.</summary>
    <description>
agent-brain is a local CLI and MCP server for AI coding agents. It indexes a repository, stores local metadata in SQLite, builds a technical graph in Neo4j, and generates compact context packs so agents can work with fewer tokens and better engineering judgment.
    </description>
    <releaseNotes>https://github.com/NanoCycles/agent-brain/releases/tag/v__VERSION__</releaseNotes>
  </metadata>
  <files>
    <file src="tools\**" target="tools" />
  </files>
</package>
