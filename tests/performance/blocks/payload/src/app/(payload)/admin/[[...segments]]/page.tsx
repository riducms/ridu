import config from "@payload-config";
import { generatePageMetadata, RootPage } from "@payloadcms/next/views";
import type { Metadata } from "next";

import { importMap } from "../importMap.js";

type Props = {
	params: Promise<{ segments: string[] }>;
	searchParams: Promise<Record<string, string | string[]>>;
};

export const generateMetadata = ({ params, searchParams }: Props): Promise<Metadata> =>
	generatePageMetadata({ config, params, searchParams });

const Page = ({ params, searchParams }: Props) =>
	RootPage({ config, importMap, params, searchParams });

export default Page;
