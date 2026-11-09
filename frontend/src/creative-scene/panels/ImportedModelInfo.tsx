import { importSource, type ModelImport } from '../models/generator.ts'

// An imported model in the properties panel: where it came from, in place of
// the parameters and code a code model has (data: 模型内容「导入的模型」).

export function ImportedModelInfo({ source }: { source: ModelImport | undefined }) {
  const s = source ? importSource(source) : null
  return (
    <fieldset className="cc-scene-model-params">
      <legend>导入的模型</legend>
      {s ? (
        <dl className="cc-scene-import-source">
          <dt>原文件</dt>
          <dd>{s.fileName}</dd>
          {s.attribution && (
            <>
              <dt>授权说明</dt>
              <dd>{s.attribution}</dd>
            </>
          )}
          {s.sourceURL && (
            <>
              <dt>来源页面</dt>
              <dd>
                <a href={s.sourceURL} target="_blank" rel="noopener noreferrer">
                  {s.sourceURL}
                </a>
              </dd>
            </>
          )}
        </dl>
      ) : (
        <small>外部导入，没有可调参数</small>
      )}
    </fieldset>
  )
}
