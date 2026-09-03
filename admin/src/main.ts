import { createAdminClient } from "@admin/core/api/admin-client";
import { mountAdmin } from "@admin/index";

const target = document.getElementById("app");

if (target === null) {
	throw new Error('Ridu admin could not find its required "#app" mount element.');
}

const app = mountAdmin({ target, clientFactory: createAdminClient });

export default app;
