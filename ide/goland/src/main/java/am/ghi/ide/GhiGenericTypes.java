package am.ghi.ide;

import java.util.*;

/** Token substitution changes types, never parameter names, comments or defaults. */
final class GhiGenericTypes {
    static List<String> arguments(String type) {
        int open=type.indexOf('[');if(open<0||!type.endsWith("]"))return List.of();
        return split(type.substring(open+1,type.length()-1));
    }
    static String base(String type){int index=type.indexOf('[');return index<0?type:type.substring(0,index);}
    static List<String> split(String text){
        List<String> result=new ArrayList<>();int start=0,depth=0;
        for(int i=0;i<=text.length();i++){
            if(i==text.length()||(text.charAt(i)==','&&depth==0)){String value=text.substring(start,i).trim();if(!value.isEmpty())result.add(value);start=i+1;continue;}
            char token=text.charAt(i);if(token=='['||token=='('||token=='{')depth++;if(token==']'||token==')'||token=='}')depth--;
        }
        return result;
    }
    static Map<String,String> bind(List<String> names,List<String> values){
        Map<String,String> result=new LinkedHashMap<>();for(int i=0;i<Math.min(names.size(),values.size());i++)result.put(names.get(i),values.get(i));return result;
    }
    static String substitute(String text,Map<String,String> bindings){
        var lexer=new GhiLexer();lexer.start(text);StringBuilder output=new StringBuilder();
        while(lexer.getTokenType()!=null){String token=text.substring(lexer.getTokenStart(),lexer.getTokenEnd());output.append(lexer.getTokenType()==GhiLexer.IDENTIFIER?bindings.getOrDefault(token,token):token);lexer.advance();}
        return output.toString();
    }
    static String parameter(String declaration,Map<String,String> bindings){
        var lexer=new GhiLexer();lexer.start(declaration);if(lexer.getTokenType()!=GhiLexer.IDENTIFIER)return declaration;
        int nameEnd=lexer.getTokenEnd(),equals=declaration.indexOf('=',nameEnd);if(equals<0)equals=declaration.length();
        return declaration.substring(0,nameEnd)+substitute(declaration.substring(nameEnd,equals),bindings)+declaration.substring(equals);
    }
}
