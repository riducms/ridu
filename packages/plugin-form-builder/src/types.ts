export interface FormFieldBase {
	_key?: string;
	name: string;
	label?: string;
	required?: boolean;
	width?: number;
}

export interface TextFormField extends FormFieldBase {
	blockType: "text" | "textarea";
	defaultValue?: string;
}

export interface EmailFormField extends FormFieldBase {
	blockType: "email";
}

export interface NumberFormField extends FormFieldBase {
	blockType: "number";
	defaultValue?: number;
}

export interface CheckboxFormField extends FormFieldBase {
	blockType: "checkbox";
	defaultValue?: boolean;
}

export interface DateFormField extends FormFieldBase {
	blockType: "date";
	defaultValue?: string;
}

export interface ChoiceOption {
	_key?: string;
	label: string;
	value: string;
}

export interface ChoiceFormField extends FormFieldBase {
	blockType: "radio" | "select";
	defaultValue?: string;
	placeholder?: string;
	options: ChoiceOption[];
}

export interface CountryFormField extends FormFieldBase {
	blockType: "country" | "state";
}

export interface MessageFormField {
	_key?: string;
	blockType: "message";
	message: string;
}

export interface CustomFormField extends FormFieldBase {
	blockType: string;
}

export interface UploadFormField extends FormFieldBase {
	blockType: "upload";
	uploadCollection: string;
	mimeTypes?: Array<{ _key?: string; mimeType: string }>;
	maxFileSize?: number;
	multiple?: boolean;
}

export type PriceCondition = {
	_key?: string;
	fieldToUse: string;
	condition: "equals" | "hasValue" | "notEquals";
	operator: "add" | "divide" | "multiply" | "subtract";
	valueForCondition?: string;
	valueForOperator: string;
	valueType: "static" | "valueOfField";
};

export interface PaymentFormField extends FormFieldBase {
	blockType: "payment";
	basePrice: number;
	paymentProcessor: string;
	priceConditions?: PriceCondition[];
}

export type FormField =
	| CheckboxFormField
	| ChoiceFormField
	| CountryFormField
	| DateFormField
	| EmailFormField
	| MessageFormField
	| NumberFormField
	| PaymentFormField
	| TextFormField
	| UploadFormField;

export type FormFieldDefinition = FormField | CustomFormField;

export interface FormEmail {
	_key?: string;
	bcc?: string;
	cc?: string;
	emailFrom: string;
	emailTo?: string;
	message?: string;
	replyTo?: string;
	subject: string;
}

export interface FormRedirect {
	reference?: UploadReference;
	type?: "custom" | "reference";
	url?: string;
}

export interface FormDefinition<TField extends FormFieldDefinition = FormField> {
	id: string;
	title: string;
	fields?: readonly TField[] | null;
	submitButtonLabel?: string;
	confirmationType: "message" | "redirect";
	confirmationMessage?: string;
	redirect?: FormRedirect;
	emails?: FormEmail[];
}

export interface SubmissionValue<TValue = unknown> {
	_key?: string;
	field: string;
	value: TValue;
}

export interface PolymorphicUploadReference {
	relationTo: string;
	id: string;
}

export type UploadReference = string | PolymorphicUploadReference;

export interface SubmissionUpload {
	_key?: string;
	field: string;
	value: UploadReference[];
}

export interface FormSubmissionInput<TValue = unknown> {
	form: string;
	submissionData: SubmissionValue<TValue>[];
	submissionUploads?: SubmissionUpload[];
}

export interface FormValidationIssue {
	code: "invalid" | "required";
	field: string;
	message: string;
}

export type FormValue =
	| boolean
	| number
	| string
	| readonly UploadReference[]
	| Readonly<Record<string, unknown>>
	| null
	| undefined;
export type FormValues<TValue = FormValue> = Readonly<Record<string, TValue>>;

export type Confirmation =
	{ type: "message"; message: string } | { type: "redirect"; redirect: FormRedirect };
