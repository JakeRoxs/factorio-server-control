const passwordRequirement = "Use 10–72 UTF-8 bytes. Non-ASCII characters can use more than one byte.";

const isValidPassword = value => {
    if (typeof value !== "string") return false;
    const bytes = new TextEncoder().encode(value).length;
    return bytes >= 10 && bytes <= 72;
};

module.exports = {isValidPassword, passwordRequirement};
