import { mountAdmin } from "@riducms/admin";
import { createClient } from "@riducms/sdk";
import adminConfig from "./admin.config";
const target = document.getElementById("app");
if (target === null) throw new Error('Ridu admin contract requires an element with id "app".');
mountAdmin({
	...adminConfig,
	target,
	clientFactory: () => createClient({ baseURL: window.location.origin }),
});
