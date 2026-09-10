import { mountAdmin } from "@riducms/admin";
import { createClient, type RiduConfig } from "../../generated/ridu.generated";

import adminConfig from "@/admin.config";

const target = document.getElementById("app");
if (target === null) throw new Error('Ridu admin requires an element with id "app".');

mountAdmin<RiduConfig>({
	target,
	clientFactory: () => createClient({ baseURL: window.location.origin }),
	...adminConfig,
});
