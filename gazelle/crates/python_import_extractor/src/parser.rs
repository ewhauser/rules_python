use std::{cell::RefCell, fs};
use tree_sitter::{Node, Parser};

thread_local! {
    static PARSER: RefCell<Option<Parser>> = const { RefCell::new(None) };
}

pub struct ParsedFile {
    pub modules: Vec<ParsedModule>,
    pub comments: Vec<String>,
    pub has_main: bool,
}

pub struct ParsedModule {
    pub name: String,
    pub line_number: u32,
    pub from: String,
    pub type_checking_only: bool,
}

pub fn parse_file(path: &str) -> Result<ParsedFile, String> {
    let source = fs::read(path).map_err(|err| format!("failed to read {path}: {err}"))?;
    parse_source(&source)
}

fn parse_source(source: &[u8]) -> Result<ParsedFile, String> {
    PARSER.with(|parser_cell| {
        let mut parser_slot = parser_cell.borrow_mut();
        if parser_slot.is_none() {
            let mut parser = Parser::new();
            parser
                .set_language(&tree_sitter_python::LANGUAGE.into())
                .map_err(|err| format!("failed to set Python grammar: {err}"))?;
            *parser_slot = Some(parser);
        }

        let tree = parser_slot
            .as_mut()
            .expect("thread-local parser initialized")
            .parse(source, None)
            .ok_or_else(|| "failed to parse Python source".to_owned())?;
        let root = tree.root_node();
        let mut out = ParsedFile {
            modules: Vec::with_capacity(8),
            comments: Vec::with_capacity(2),
            has_main: parse_main(root, source),
        };
        parse_node(root, source, false, &mut out);
        Ok(out)
    })
}

fn parse_node(node: Node<'_>, source: &[u8], in_type_checking_block: bool, out: &mut ParsedFile) {
    let next_type_checking = in_type_checking_block || is_type_checking_block(node, source);

    for idx in 0..node.child_count() {
        let Some(child) = node.child(idx) else {
            continue;
        };
        if parse_import_statement(child, source, next_type_checking, out)
            || parse_comment(child, source, out)
        {
            continue;
        }
        parse_node(child, source, next_type_checking, out);
    }
}

fn parse_import_statement(
    node: Node<'_>,
    source: &[u8],
    type_checking_only: bool,
    out: &mut ParsedFile,
) -> bool {
    match node.kind() {
        "import_statement" => {
            for idx in 1..node.child_count() {
                let Some(child) = node.child(idx) else {
                    continue;
                };
                let Some(name) = parse_import_node(child, source) else {
                    continue;
                };
                if name.starts_with('.') {
                    continue;
                }
                out.modules.push(ParsedModule {
                    name,
                    line_number: (child.start_position().row + 1) as u32,
                    from: String::new(),
                    type_checking_only,
                });
            }
            true
        }
        "import_from_statement" => {
            let Some(from_node) = node.child(1) else {
                return true;
            };
            let from = clean_import_string(node_text(from_node, source));
            if from == "." {
                return true;
            }
            for idx in 3..node.child_count() {
                let Some(child) = node.child(idx) else {
                    continue;
                };
                let Some(imported) = parse_import_node(child, source) else {
                    continue;
                };
                out.modules.push(ParsedModule {
                    name: format!("{from}.{imported}"),
                    line_number: (child.start_position().row + 1) as u32,
                    from: from.clone(),
                    type_checking_only,
                });
            }
            true
        }
        _ => false,
    }
}

fn parse_import_node(node: Node<'_>, source: &[u8]) -> Option<String> {
    match node.kind() {
        "dotted_name" => Some(clean_import_string(node_text(node, source))),
        "aliased_import" => node
            .child(0)
            .and_then(|child| parse_import_node(child, source)),
        "wildcard_import" => Some("*".to_owned()),
        _ => None,
    }
}

fn parse_comment(node: Node<'_>, source: &[u8], out: &mut ParsedFile) -> bool {
    if node.kind() != "comment" {
        return false;
    }
    out.comments.push(node_text(node, source).to_owned());
    true
}

fn is_type_checking_block(node: Node<'_>, source: &[u8]) -> bool {
    if node.kind() != "if_statement" || node.child_count() < 2 {
        return false;
    }
    let Some(condition) = node.child(1) else {
        return false;
    };
    if condition.kind() == "identifier" && node_text(condition, source) == "TYPE_CHECKING" {
        return true;
    }
    if condition.kind() == "attribute" && condition.child_count() >= 3 {
        let Some(object) = condition.child(0) else {
            return false;
        };
        let Some(attr) = condition.child(2) else {
            return false;
        };
        return object.kind() == "identifier"
            && node_text(object, source) == "typing"
            && attr.kind() == "identifier"
            && node_text(attr, source) == "TYPE_CHECKING";
    }
    false
}

fn parse_main(root: Node<'_>, source: &[u8]) -> bool {
    for idx in 0..root.child_count() {
        let Some(child) = root.child(idx) else {
            continue;
        };
        if child.kind() != "if_statement" {
            continue;
        }
        let Some(statement) = child.child(1) else {
            continue;
        };
        if statement.kind() != "comparison_operator" {
            continue;
        }
        let Some(operator) = statement.child(1) else {
            continue;
        };
        if node_text(operator, source) != "==" {
            continue;
        }
        let (Some(mut a), Some(mut b)) = (statement.child(0), statement.child(2)) else {
            continue;
        };
        if b.kind() == "identifier" {
            std::mem::swap(&mut a, &mut b);
        }
        if a.kind() == "identifier"
            && node_text(a, source) == "__name__"
            && b.kind() == "string"
            && string_value(b, source) == "__main__"
        {
            return true;
        }
    }
    false
}

fn string_value(node: Node<'_>, source: &[u8]) -> String {
    node_text(node, source)
        .trim_matches('"')
        .trim_matches('\'')
        .to_owned()
}

fn clean_import_string(s: &str) -> String {
    s.replace(['\r', '\n', '\\', ' ', '\t'], "")
}

fn node_text<'a>(node: Node<'a>, source: &'a [u8]) -> &'a str {
    node.utf8_text(source).unwrap_or("")
}
