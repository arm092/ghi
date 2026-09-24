package am.ghi.ide;

import com.intellij.openapi.command.WriteCommandAction;
import com.intellij.openapi.editor.Editor;
import com.intellij.psi.*;
import com.intellij.psi.util.PsiTreeUtil;
import java.util.*;

/** Explicit import edits shared by completion and intentions. Never edits while typing. */
final class GhiTypeImports {
    record Choice(GhiSymbols.Symbol symbol,String path) { @Override public String toString(){return path;} }
    record Site(int start,int end,String text) {}
    static List<Choice> candidates(PsiFile file,String name){
        var model=GhiSymbols.forFile(file);List<Choice> result=new ArrayList<>();
        for(var symbol:model.symbols)if(Set.of("class","interface","type").contains(symbol.kind)&&symbol.scope.parent==null
            &&(name==null||symbol.name.equals(name))){
            String namespace=model.sources.get(symbol.file).namespace;
            if(!namespace.isEmpty())result.add(new Choice(symbol,namespace+"."+symbol.name));
        }
        return result;
    }
    static Site site(PsiFile file,int offset,boolean qualified){
        var leaf=file.findElementAt(Math.min(offset,Math.max(0,file.getTextLength()-1)));
        var id=PsiTreeUtil.getParentOfType(leaf,GhiIdentifier.class,false);
        if(id==null&&offset>0)id=PsiTreeUtil.getParentOfType(file.findElementAt(offset-1),GhiIdentifier.class,false);
        if(id==null)return null;
        String text=file.getText();int start=id.getTextOffset(),end=start+id.getTextLength();
        if(qualified){while(start>0&&(Character.isUnicodeIdentifierPart(text.charAt(start-1))||text.charAt(start-1)=='.'))start--;
            while(end<text.length()&&(Character.isUnicodeIdentifierPart(text.charAt(end))||text.charAt(end)=='.'))end++;
            if(!text.substring(start,end).contains("."))return null;
            var resolved=GhiSymbols.forFile(file).symbolAt(file,end-1);
            if(resolved!=null&&!Set.of("class","interface","type").contains(resolved.kind))return null;
        }else if((start>0&&text.charAt(start-1)=='.')||GhiSymbols.forFile(file).resolve(file,start)!=null)return null;
        int lineStart=text.lastIndexOf('\n',start)+1;
        if(text.substring(lineStart,start).stripLeading().startsWith("import ")||text.substring(lineStart,start).stripLeading().startsWith("namespace "))return null;
        return new Site(start,end,text.substring(start,end));
    }
    static void apply(PsiFile file,Editor editor,Site site,Choice choice){
        WriteCommandAction.runWriteCommandAction(file.getProject(),()->{
            var document=PsiDocumentManager.getInstance(file.getProject()).getDocument(file);if(document==null)return;
            var model=GhiSymbols.forFile(file);String local=choice.symbol.name;boolean present=false;
            for(var symbol:model.symbols)if(symbol.file==file&&symbol.kind.equals("typeImport")&&symbol.importNamespace.equals(choice.path)){
                local=symbol.name;present=true;break;
            }
            Set<String> occupied=new HashSet<>();
            for(var symbol:model.symbols)if(symbol.file==file||(symbol.scope.parent==null&&model.sources.get(symbol.file).namespace.equals(model.sources.get(file).namespace))){
                occupied.add(symbol.name);occupied.addAll(symbol.typeParameters);
                if(present&&symbol.file==file&&((symbol.scope.parent!=null&&symbol.scope.contains(site.start)&&symbol.name.equals(local))
                    ||(symbol.body!=null&&symbol.body.contains(site.start)&&symbol.typeParameters.contains(local))))present=false;
            }
            if(!present&&occupied.contains(local)){
                String namespace=choice.path.substring(0,choice.path.lastIndexOf('.'));String qualifier=namespace.substring(namespace.lastIndexOf('.')+1);
                String base=Character.toUpperCase(qualifier.charAt(0))+qualifier.substring(1)+local;local=base;int suffix=2;
                while(occupied.contains(local))local=base+suffix++;
            }
            document.replaceString(site.start,site.end,local);
            int added=0;
            if(!present){
                // Only lexer tokens at file scope can anchor an import; text in comments
                // or multiline literals must never become an insertion point.
                var source=model.sources.get(file);var tokens=source.tokens;int insert=0;
                for(int at=0;at<tokens.size();at++)if(source.scope(tokens.get(at).start()).parent==null
                    &&Set.of("namespace","import").contains(tokens.get(at).text())){
                    int end=at+1;if(end>=tokens.size())continue;
                    if(tokens.get(end).identifier()){
                        if(end+1<tokens.size()&&tokens.get(end+1).text().startsWith("\""))end++;
                        else {
                            while(GhiSymbols.is(tokens,end+1,".")&&end+2<tokens.size()&&tokens.get(end+2).identifier())end+=2;
                            if(GhiSymbols.is(tokens,end+1,"as")&&end+2<tokens.size()&&tokens.get(end+2).identifier())end+=2;
                        }
                    }
                    if(GhiSymbols.is(tokens,end+1,";"))end++;
                    insert=tokens.get(end).end();
                }
                if(insert>site.end)insert+=local.length()-(site.end-site.start);
                int newline=document.getText().indexOf('\n',insert);
                if(newline>=0&&document.getText().substring(insert,newline).stripLeading().startsWith("//"))insert=newline;
                String statement=(insert==0?"":"\n")+"import "+choice.path+(local.equals(choice.symbol.name)?"":" as "+local)+(insert==0?"\n":"");
                document.insertString(insert,statement);if(insert<=site.start)added=statement.length();
            }
            PsiDocumentManager.getInstance(file.getProject()).commitDocument(document);
            if(editor!=null)editor.getCaretModel().moveToOffset(site.start+local.length()+added);
        });
    }
}
