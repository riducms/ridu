import type { ShikiTransformer } from 'shiki';
import {
	parseCodeLineAnnotations,
	validateCodeLineAnnotations,
	type CodeLineAnnotation
} from '@/plugins/code-line-annotations';

interface RiduTransformerMeta {
	riduLineAnnotations?: CodeLineAnnotation[];
}

function annotations(meta: object): CodeLineAnnotation[] {
	return (meta as RiduTransformerMeta).riduLineAnnotations ?? [];
}

/** Applies fenced `add`, `remove`, and `focus` selectors after Shiki creates its line spans. */
export const riduCodeLineTransformer: ShikiTransformer = {
	name: 'ridu-code-line-annotations',
	preprocess(code, options) {
		const raw = options.meta?.__raw;
		const lineCount = code === '' ? 0 : code.split('\n').length;
		const annotationMeta = typeof raw === 'string' ? raw : '';
		const diagnostics = validateCodeLineAnnotations(annotationMeta, lineCount);
		if (diagnostics.length > 0) {
			throw new Error(`Invalid code line annotations: ${diagnostics.join('; ')}`);
		}
		(this.meta as RiduTransformerMeta).riduLineAnnotations = parseCodeLineAnnotations(
			annotationMeta,
			lineCount
		);
	},
	pre(node) {
		if (annotations(this.meta).some((line) => line.classes.length > 0)) {
			this.addClassToHast(node, 'has-line-annotations');
		}
	},
	line(node, lineNumber) {
		const annotation = annotations(this.meta)[lineNumber - 1];
		if (!annotation || annotation.classes.length === 0) return;

		this.addClassToHast(node, annotation.classes);
		node.properties['data-line-number'] = annotation.lineNumber;
		if (annotation.marker) node.properties['data-line-marker'] = annotation.marker;
	}
};
