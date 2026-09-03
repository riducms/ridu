import config from "@payload-config";
import { generatePageMetadata, NotFoundPage } from "@payloadcms/next/views";
import type { Metadata } from "next";

import { importMap } from "../importMap.js";

type Props = {
	params: Promise<{ segments: string[] }>;
	searchParams: Promise<Record<string, string | string[]>>;
};

export const generateMetadata = ({ params, searchParams }: Props): Promise<Metadata> =>
	generatePageMetadata({ config, params, searchParams });

const NotFound = ({ params, searchParams }: Props) =>
	NotFoundPage({ config, importMap, params, searchParams });

export default NotFound;
